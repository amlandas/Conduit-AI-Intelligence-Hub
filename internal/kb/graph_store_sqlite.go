// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// graph_store_sqlite.go implements the knowledge graph on plain SQLite.
//
// This replaces the FalkorDB (Redis + Cypher) store deleted in WP-2.3. The July
// audit found the external graph database to be the highest-cost, lowest-validated
// component in the stack: its result parsers were never implemented (every
// traversal returned an empty slice), so no query ever benefited from it, while
// it cost a container, a loopback port with no authentication, and a dependency
// on github.com/redis/go-redis.
//
// What replaces it is deliberately small -- LazyGraphRAG-style minimal edges:
// a typed (subject, predicate, object) table with provenance and a confidence
// score, traversed one or two hops with ordinary indexed SQL. It lives in the
// same SQLite file as the rest of the knowledge base, so a graph query is a
// local read with no network, no daemon, and no extra process.
//
// The whole feature is off by default. NewGraphStore(db, false) returns a store
// that creates nothing and writes nothing; every mutating call is a no-op and
// every read returns empty. Enabling it (kb.kag.enabled) is what creates the
// tables, on first use, via EnsureSchema.
package kb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MaxGraphHops is the traversal budget for the SQLite graph.
//
// Two hops is the LazyGraphRAG sweet spot: it covers "what is connected to the
// things this query matched, and what is connected to those" while keeping the
// frontier small enough that each hop stays an indexed lookup. Deeper traversal
// is the kind of rich-graph work the evidence gate governs -- re-open it only if
// the local query log shows real multi-hop demand.
const MaxGraphHops = 2

// Traversal budgets. These bound a single kag_query so a pathological hub entity
// cannot turn one tool call into a table scan.
const (
	// maxGraphFrontier caps how many entities each hop expands from.
	maxGraphFrontier = 128

	// maxGraphEdges caps how many edges a single traversal returns.
	maxGraphEdges = 200
)

// GraphStore is the SQLite-backed knowledge graph.
//
// The zero value is not usable; construct with NewGraphStore. A store whose
// enabled flag is false is inert: it never creates tables, never writes, and
// returns empty results. That is the default configuration.
type GraphStore struct {
	db      *sql.DB
	enabled bool
}

// NewGraphStore creates a graph store over an existing knowledge base database.
//
// enabled mirrors kb.kag.enabled. When it is false the returned store is inert;
// callers do not need to nil-check it or branch on configuration.
func NewGraphStore(db *sql.DB, enabled bool) *GraphStore {
	return &GraphStore{db: db, enabled: enabled}
}

// Enabled reports whether the knowledge graph is turned on.
func (g *GraphStore) Enabled() bool {
	return g != nil && g.enabled && g.db != nil
}

// EnsureSchema creates the graph tables if they do not exist.
//
// This is the only place graph storage comes into being: a Conduit install that
// never enables KAG never gets these tables. It is idempotent and safe to call
// on every enabled startup.
//
// kb_entities is created without foreign keys on purpose. On an existing install
// the table already exists (created by store migration 004, with FK cascades onto
// kb_chunks and kb_documents) and this statement is a no-op; the FK-free form
// exists only so an enabled graph is self-sufficient in a database that has not
// run that migration -- for example a test fixture.
func (g *GraphStore) EnsureSchema(ctx context.Context) error {
	if !g.Enabled() {
		return nil
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kb_entities (
			entity_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			description TEXT,
			source_chunk_id TEXT,
			source_document_id TEXT,
			confidence REAL NOT NULL DEFAULT 0.0,
			metadata TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		// Typed edges. subject/predicate/object plus the chunk and document the
		// edge was read out of, so every traversal result can be traced back to
		// source text, and a confidence score so low-quality edges can be
		// filtered at query time rather than at write time.
		`CREATE TABLE IF NOT EXISTS kb_graph_edges (
			edge_id            TEXT PRIMARY KEY,
			subject_id         TEXT NOT NULL,
			predicate          TEXT NOT NULL,
			object_id          TEXT NOT NULL,
			source_chunk_id    TEXT NOT NULL DEFAULT '',
			source_document_id TEXT NOT NULL DEFAULT '',
			confidence         REAL NOT NULL DEFAULT 0.0,
			created_at         TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_graph_edges_subject ON kb_graph_edges(subject_id)`,
		`CREATE INDEX IF NOT EXISTS idx_graph_edges_object ON kb_graph_edges(object_id)`,
		`CREATE INDEX IF NOT EXISTS idx_graph_edges_predicate ON kb_graph_edges(predicate)`,
		`CREATE INDEX IF NOT EXISTS idx_graph_edges_document ON kb_graph_edges(source_document_id)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_name ON kb_entities(name)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_type ON kb_entities(type)`,
	}

	for _, stmt := range stmts {
		if _, err := g.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("graph schema: %w", err)
		}
	}
	return nil
}

// UpsertEntity writes an entity, merging with any existing row for the same
// canonical ID.
//
// Merge rules match the pre-existing SQLite entity path: keep the longer
// description (more informative), keep the higher confidence, and accumulate
// source document IDs as a comma-separated list.
func (g *GraphStore) UpsertEntity(ctx context.Context, e *Entity) error {
	if !g.Enabled() {
		return nil
	}
	if err := ValidateEntity(e); err != nil {
		return err
	}

	id := e.ID
	if id == "" {
		id = GenerateCanonicalEntityID(e.Name, e.Type)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var existingDesc, existingSources string
	var existingConfidence float64
	err := g.db.QueryRowContext(ctx, `
		SELECT COALESCE(description, ''), confidence, COALESCE(source_document_id, '')
		FROM kb_entities WHERE entity_id = ?
	`, id).Scan(&existingDesc, &existingConfidence, &existingSources)

	switch {
	case err == sql.ErrNoRows:
		_, err = g.db.ExecContext(ctx, `
			INSERT INTO kb_entities
			(entity_id, name, type, description, source_chunk_id, source_document_id, confidence, metadata, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)
		`, id, e.Name, string(e.Type), e.Description, e.SourceChunkID, e.SourceDocumentID, e.Confidence, now, now)
		return err
	case err != nil:
		return err
	}

	desc := existingDesc
	if len(e.Description) > len(desc) {
		desc = e.Description
	}
	confidence := existingConfidence
	if e.Confidence > confidence {
		confidence = e.Confidence
	}
	sources := mergeSourceList(existingSources, e.SourceDocumentID)

	_, err = g.db.ExecContext(ctx, `
		UPDATE kb_entities
		SET name = ?, type = ?, description = ?, source_chunk_id = ?,
		    source_document_id = ?, confidence = ?, updated_at = ?
		WHERE entity_id = ?
	`, e.Name, string(e.Type), desc, e.SourceChunkID, sources, confidence, now, id)
	return err
}

// mergeSourceList appends id to a comma-separated list if not already present.
func mergeSourceList(existing, id string) string {
	if id == "" {
		return existing
	}
	for _, part := range strings.Split(existing, ",") {
		if strings.TrimSpace(part) == id {
			return existing
		}
	}
	if existing == "" {
		return id
	}
	return existing + "," + id
}

// UpsertEdge writes a typed edge between two entities.
//
// Edges are keyed on (subject, predicate, object), so re-extracting the same
// chunk refreshes an edge rather than duplicating it. Self-edges are rejected:
// they carry no traversal information and only inflate the frontier.
func (g *GraphStore) UpsertEdge(ctx context.Context, r *Relation) error {
	if !g.Enabled() {
		return nil
	}
	if err := ValidateRelation(r); err != nil {
		return err
	}
	if r.SubjectID == r.ObjectID {
		return ErrSelfRelation
	}

	id := r.ID
	if id == "" {
		id = GenerateRelationID(r.SubjectID, string(r.Predicate), r.ObjectID)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := g.db.ExecContext(ctx, `
		INSERT INTO kb_graph_edges
		(edge_id, subject_id, predicate, object_id, source_chunk_id, source_document_id, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(edge_id) DO UPDATE SET
			source_chunk_id    = excluded.source_chunk_id,
			source_document_id = excluded.source_document_id,
			confidence         = MAX(kb_graph_edges.confidence, excluded.confidence)
	`, id, r.SubjectID, string(r.Predicate), r.ObjectID,
		r.SourceChunkID, r.SourceDocumentID, r.Confidence, now)
	return err
}

// GetEntity returns a single entity by ID, or ErrEntityNotFound.
func (g *GraphStore) GetEntity(ctx context.Context, entityID string) (*Entity, error) {
	if !g.Enabled() {
		return nil, ErrEntityNotFound
	}

	var e Entity
	var typ string
	err := g.db.QueryRowContext(ctx, `
		SELECT entity_id, name, type, COALESCE(description, ''),
		       COALESCE(source_chunk_id, ''), COALESCE(source_document_id, ''), confidence
		FROM kb_entities WHERE entity_id = ?
	`, entityID).Scan(&e.ID, &e.Name, &typ, &e.Description,
		&e.SourceChunkID, &e.SourceDocumentID, &e.Confidence)

	if err == sql.ErrNoRows {
		return nil, ErrEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Type = EntityType(typ)
	return &e, nil
}

// GraphEdge is one traversal result: a typed edge with resolved endpoint names
// and the hop at which it was discovered.
type GraphEdge struct {
	SubjectID        string
	SubjectName      string
	Predicate        string
	ObjectID         string
	ObjectName       string
	Confidence       float64
	SourceChunkID    string
	SourceDocumentID string
	Hop              int
}

// Traverse walks the graph outward from a set of seed entities.
//
// It performs a breadth-first expansion of at most maxHops rounds (clamped to
// MaxGraphHops), returning every edge encountered along with the set of entity
// IDs reached. Edges are undirected for traversal purposes -- an entity is
// reachable through an edge whether it is the subject or the object -- because
// "what is this connected to" is the question a knowledge-graph query actually
// asks.
//
// The returned edges are ordered by hop, then by confidence descending, so the
// closest and best-supported connections come first when a caller truncates.
func (g *GraphStore) Traverse(ctx context.Context, seedIDs []string, maxHops int) ([]GraphEdge, []string, error) {
	if !g.Enabled() || len(seedIDs) == 0 {
		return nil, nil, nil
	}
	if maxHops <= 0 {
		maxHops = 1
	}
	if maxHops > MaxGraphHops {
		maxHops = MaxGraphHops
	}

	visited := make(map[string]bool, len(seedIDs))
	frontier := make([]string, 0, len(seedIDs))
	for _, id := range seedIDs {
		if id == "" || visited[id] {
			continue
		}
		visited[id] = true
		frontier = append(frontier, id)
	}
	if len(frontier) > maxGraphFrontier {
		frontier = frontier[:maxGraphFrontier]
	}

	var edges []GraphEdge
	seenEdge := make(map[string]bool)

	for hop := 1; hop <= maxHops && len(frontier) > 0 && len(edges) < maxGraphEdges; hop++ {
		hopEdges, err := g.edgesTouching(ctx, frontier)
		if err != nil {
			return nil, nil, err
		}

		var next []string
		for _, e := range hopEdges {
			if seenEdge[e.edgeID] {
				continue
			}
			seenEdge[e.edgeID] = true

			e.edge.Hop = hop
			edges = append(edges, e.edge)
			if len(edges) >= maxGraphEdges {
				break
			}

			for _, endpoint := range []string{e.edge.SubjectID, e.edge.ObjectID} {
				if endpoint != "" && !visited[endpoint] {
					visited[endpoint] = true
					if len(next) < maxGraphFrontier {
						next = append(next, endpoint)
					}
				}
			}
		}
		frontier = next
	}

	reached := make([]string, 0, len(visited))
	for id := range visited {
		reached = append(reached, id)
	}
	return edges, reached, nil
}

// hopEdge pairs an edge with its primary key so the traversal can dedupe.
type hopEdge struct {
	edgeID string
	edge   GraphEdge
}

// edgesTouching returns every edge with an endpoint in ids, best-supported first.
func (g *GraphStore) edgesTouching(ctx context.Context, ids []string) ([]hopEdge, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)*2+1)
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, maxGraphEdges)

	query := fmt.Sprintf(`
		SELECT e.edge_id, e.subject_id, COALESCE(se.name, e.subject_id),
		       e.predicate, e.object_id, COALESCE(oe.name, e.object_id),
		       e.confidence, e.source_chunk_id, e.source_document_id
		FROM kb_graph_edges e
		LEFT JOIN kb_entities se ON e.subject_id = se.entity_id
		LEFT JOIN kb_entities oe ON e.object_id = oe.entity_id
		WHERE e.subject_id IN (%s) OR e.object_id IN (%s)
		ORDER BY e.confidence DESC, e.edge_id
		LIMIT ?
	`, placeholders, placeholders)

	rows, err := g.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []hopEdge
	for rows.Next() {
		var h hopEdge
		if err := rows.Scan(&h.edgeID, &h.edge.SubjectID, &h.edge.SubjectName,
			&h.edge.Predicate, &h.edge.ObjectID, &h.edge.ObjectName,
			&h.edge.Confidence, &h.edge.SourceChunkID, &h.edge.SourceDocumentID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteByDocument removes every edge extracted from a document and returns the
// number deleted. Entities are left alone: they are canonical across documents,
// so deleting one document must not evict an entity another document also
// mentions.
func (g *GraphStore) DeleteByDocument(ctx context.Context, documentID string) (int, error) {
	if !g.Enabled() {
		return 0, nil
	}

	res, err := g.db.ExecContext(ctx,
		`DELETE FROM kb_graph_edges WHERE source_document_id = ?`, documentID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// Stats returns entity and edge counts, broken down by type.
func (g *GraphStore) Stats(ctx context.Context) (*GraphStats, error) {
	stats := &GraphStats{
		EntitiesByType:  make(map[EntityType]int64),
		RelationsByType: make(map[RelationType]int64),
		LastUpdated:     time.Now().UTC(),
	}
	if !g.Enabled() {
		return stats, nil
	}

	// A disabled-then-enabled install may not have run EnsureSchema yet; missing
	// tables report zero rather than erroring.
	_ = g.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_entities`).Scan(&stats.TotalEntities)
	_ = g.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_graph_edges`).Scan(&stats.TotalRelations)

	if rows, err := g.db.QueryContext(ctx, `SELECT type, COUNT(*) FROM kb_entities GROUP BY type`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			var c int64
			if err := rows.Scan(&t, &c); err == nil {
				stats.EntitiesByType[EntityType(t)] = c
			}
		}
	}

	if rows, err := g.db.QueryContext(ctx, `SELECT predicate, COUNT(*) FROM kb_graph_edges GROUP BY predicate`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var p string
			var c int64
			if err := rows.Scan(&p, &c); err == nil {
				stats.RelationsByType[RelationType(p)] = c
			}
		}
	}

	return stats, nil
}

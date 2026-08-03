package kb

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newGraphDB opens an empty SQLite database with nothing in it. The point is
// that the graph store must be able to stand up its own schema.
func newGraphDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newEnabledGraph returns a graph store with its schema created.
func newEnabledGraph(t *testing.T) (*GraphStore, *sql.DB) {
	t.Helper()
	db := newGraphDB(t)
	g := NewGraphStore(db, true)
	if err := g.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return g, db
}

func mustUpsertEntity(t *testing.T, g *GraphStore, id, name string, typ EntityType, confidence float64) {
	t.Helper()
	err := g.UpsertEntity(context.Background(), &Entity{
		ID:               id,
		Name:             name,
		Type:             typ,
		Description:      name + " description",
		SourceDocumentID: "doc1",
		SourceChunkID:    "chunk1",
		Confidence:       confidence,
	})
	if err != nil {
		t.Fatalf("upsert entity %s: %v", id, err)
	}
}

func mustUpsertEdge(t *testing.T, g *GraphStore, subject string, predicate RelationType, object, docID string, confidence float64) {
	t.Helper()
	err := g.UpsertEdge(context.Background(), &Relation{
		SubjectID:        subject,
		Predicate:        predicate,
		ObjectID:         object,
		SourceChunkID:    "chunk1",
		SourceDocumentID: docID,
		Confidence:       confidence,
	})
	if err != nil {
		t.Fatalf("upsert edge %s-%s->%s: %v", subject, predicate, object, err)
	}
}

// TestGraphStoreDisabledIsInert is the central guarantee of the off-by-default
// design: a disabled store must leave no trace in the database at all.
func TestGraphStoreDisabledIsInert(t *testing.T) {
	db := newGraphDB(t)
	g := NewGraphStore(db, false)
	ctx := context.Background()

	if g.Enabled() {
		t.Fatal("store constructed with enabled=false reports enabled")
	}

	if err := g.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema on disabled store: %v", err)
	}

	// No tables may have been created.
	for _, table := range []string{"kb_graph_edges", "kb_entities"} {
		var n int
		err := db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("disabled store created table %s", table)
		}
	}

	// Mutations are silent no-ops, not errors: callers should not have to
	// branch on configuration.
	if err := g.UpsertEntity(ctx, &Entity{Name: "X", Type: EntityTypeConcept, Confidence: 0.9}); err != nil {
		t.Errorf("UpsertEntity on disabled store: %v", err)
	}
	if err := g.UpsertEdge(ctx, &Relation{
		SubjectID: "a", ObjectID: "b", Predicate: RelationRelatesTo, Confidence: 0.9,
	}); err != nil {
		t.Errorf("UpsertEdge on disabled store: %v", err)
	}

	edges, reached, err := g.Traverse(ctx, []string{"a"}, 2)
	if err != nil {
		t.Errorf("Traverse on disabled store: %v", err)
	}
	if len(edges) != 0 || len(reached) != 0 {
		t.Errorf("disabled store returned %d edges / %d entities", len(edges), len(reached))
	}

	if n, err := g.DeleteByDocument(ctx, "doc1"); err != nil || n != 0 {
		t.Errorf("DeleteByDocument on disabled store = %d, %v", n, err)
	}

	stats, err := g.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats on disabled store: %v", err)
	}
	if stats.TotalEntities != 0 || stats.TotalRelations != 0 {
		t.Errorf("disabled store reported %d entities / %d relations",
			stats.TotalEntities, stats.TotalRelations)
	}
}

// TestGraphStoreNilReceiverIsInert covers the nil-store path used by callers
// that never configure a graph at all.
func TestGraphStoreNilReceiverIsInert(t *testing.T) {
	var g *GraphStore
	if g.Enabled() {
		t.Fatal("nil store reports enabled")
	}
	if err := g.UpsertEntity(context.Background(), &Entity{Name: "X"}); err != nil {
		t.Errorf("UpsertEntity on nil store: %v", err)
	}
}

func TestGraphStoreEntityCRUD(t *testing.T) {
	g, _ := newEnabledGraph(t)
	ctx := context.Background()

	mustUpsertEntity(t, g, "ent1", "Kubernetes", EntityTypeTechnology, 0.8)

	got, err := g.GetEntity(ctx, "ent1")
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	if got.Name != "Kubernetes" || got.Type != EntityTypeTechnology {
		t.Errorf("got %s/%s, want Kubernetes/technology", got.Name, got.Type)
	}
	if got.Confidence != 0.8 {
		t.Errorf("confidence = %v, want 0.8", got.Confidence)
	}

	// Re-upsert with higher confidence and a longer description: both should
	// win, and the source document list should accumulate.
	err = g.UpsertEntity(ctx, &Entity{
		ID:               "ent1",
		Name:             "Kubernetes",
		Type:             EntityTypeTechnology,
		Description:      strings.Repeat("longer description ", 5),
		SourceDocumentID: "doc2",
		Confidence:       0.95,
	})
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	got, err = g.GetEntity(ctx, "ent1")
	if err != nil {
		t.Fatalf("get after merge: %v", err)
	}
	if got.Confidence != 0.95 {
		t.Errorf("merged confidence = %v, want 0.95", got.Confidence)
	}
	if !strings.Contains(got.Description, "longer description") {
		t.Errorf("merge kept the shorter description: %q", got.Description)
	}
	if !strings.Contains(got.SourceDocumentID, "doc1") || !strings.Contains(got.SourceDocumentID, "doc2") {
		t.Errorf("source documents = %q, want both doc1 and doc2", got.SourceDocumentID)
	}

	if _, err := g.GetEntity(ctx, "nope"); err != ErrEntityNotFound {
		t.Errorf("GetEntity(missing) = %v, want ErrEntityNotFound", err)
	}
}

func TestGraphStoreEdgeCRUD(t *testing.T) {
	g, db := newEnabledGraph(t)
	ctx := context.Background()

	mustUpsertEntity(t, g, "ent1", "Kubernetes", EntityTypeTechnology, 0.9)
	mustUpsertEntity(t, g, "ent2", "Container", EntityTypeConcept, 0.9)

	mustUpsertEdge(t, g, "ent1", RelationRelatesTo, "ent2", "doc1", 0.7)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kb_graph_edges`).Scan(&count); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if count != 1 {
		t.Fatalf("edge count = %d, want 1", count)
	}

	// Re-extracting the same chunk must refresh, not duplicate, and must keep
	// the higher confidence.
	mustUpsertEdge(t, g, "ent1", RelationRelatesTo, "ent2", "doc1", 0.9)
	if err := db.QueryRow(`SELECT COUNT(*) FROM kb_graph_edges`).Scan(&count); err != nil {
		t.Fatalf("recount edges: %v", err)
	}
	if count != 1 {
		t.Errorf("re-upsert duplicated the edge: count = %d", count)
	}
	var confidence float64
	if err := db.QueryRow(`SELECT confidence FROM kb_graph_edges`).Scan(&confidence); err != nil {
		t.Fatalf("read confidence: %v", err)
	}
	if confidence != 0.9 {
		t.Errorf("confidence = %v, want 0.9 (max of both writes)", confidence)
	}

	// Provenance columns must actually be populated: they are the whole reason
	// this table exists rather than reusing kb_relations.
	var chunkID, docID string
	if err := db.QueryRow(
		`SELECT source_chunk_id, source_document_id FROM kb_graph_edges`).Scan(&chunkID, &docID); err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	if chunkID != "chunk1" || docID != "doc1" {
		t.Errorf("provenance = %q/%q, want chunk1/doc1", chunkID, docID)
	}

	// Self-edges carry no traversal information.
	err := g.UpsertEdge(ctx, &Relation{
		SubjectID: "ent1", ObjectID: "ent1", Predicate: RelationRelatesTo, Confidence: 0.9,
	})
	if err != ErrSelfRelation {
		t.Errorf("self edge accepted: %v", err)
	}
}

func TestGraphStoreDeleteByDocument(t *testing.T) {
	g, db := newEnabledGraph(t)
	ctx := context.Background()

	mustUpsertEntity(t, g, "ent1", "Alpha", EntityTypeConcept, 0.9)
	mustUpsertEntity(t, g, "ent2", "Beta", EntityTypeConcept, 0.9)
	mustUpsertEntity(t, g, "ent3", "Gamma", EntityTypeConcept, 0.9)

	mustUpsertEdge(t, g, "ent1", RelationRelatesTo, "ent2", "doc1", 0.8)
	mustUpsertEdge(t, g, "ent2", RelationRelatesTo, "ent3", "doc2", 0.8)

	n, err := g.DeleteByDocument(ctx, "doc1")
	if err != nil {
		t.Fatalf("delete by document: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d edges, want 1", n)
	}

	var remaining int
	db.QueryRow(`SELECT COUNT(*) FROM kb_graph_edges`).Scan(&remaining)
	if remaining != 1 {
		t.Errorf("%d edges remain, want 1", remaining)
	}

	// Entities are canonical across documents and must survive.
	var entities int
	db.QueryRow(`SELECT COUNT(*) FROM kb_entities`).Scan(&entities)
	if entities != 3 {
		t.Errorf("%d entities remain, want 3 (entities are canonical)", entities)
	}
}

// TestGraphStoreTraversal walks a small chain A-B-C-D and checks that hop depth
// actually limits reach.
func TestGraphStoreTraversal(t *testing.T) {
	g, _ := newEnabledGraph(t)
	ctx := context.Background()

	for _, e := range []struct {
		id, name string
	}{{"a", "Alpha"}, {"b", "Beta"}, {"c", "Gamma"}, {"d", "Delta"}} {
		mustUpsertEntity(t, g, e.id, e.name, EntityTypeConcept, 0.9)
	}

	mustUpsertEdge(t, g, "a", RelationRelatesTo, "b", "doc1", 0.9)
	mustUpsertEdge(t, g, "b", RelationRelatesTo, "c", "doc1", 0.8)
	mustUpsertEdge(t, g, "c", RelationRelatesTo, "d", "doc1", 0.7)

	t.Run("one hop reaches immediate neighbours only", func(t *testing.T) {
		edges, reached, err := g.Traverse(ctx, []string{"a"}, 1)
		if err != nil {
			t.Fatalf("traverse: %v", err)
		}
		if len(edges) != 1 {
			t.Fatalf("got %d edges, want 1", len(edges))
		}
		if edges[0].SubjectName != "Alpha" || edges[0].ObjectName != "Beta" {
			t.Errorf("edge = %s -> %s, want Alpha -> Beta", edges[0].SubjectName, edges[0].ObjectName)
		}
		if edges[0].Hop != 1 {
			t.Errorf("hop = %d, want 1", edges[0].Hop)
		}
		if containsString(reached, "c") {
			t.Error("one-hop traversal reached a two-hop entity")
		}
	})

	t.Run("two hops reach one step further", func(t *testing.T) {
		edges, reached, err := g.Traverse(ctx, []string{"a"}, 2)
		if err != nil {
			t.Fatalf("traverse: %v", err)
		}
		if len(edges) != 2 {
			t.Fatalf("got %d edges, want 2", len(edges))
		}
		if !containsString(reached, "c") {
			t.Error("two-hop traversal did not reach the two-hop entity")
		}
		if containsString(reached, "d") {
			t.Error("two-hop traversal reached a three-hop entity")
		}
	})

	t.Run("hop request above the budget is clamped", func(t *testing.T) {
		edges, reached, err := g.Traverse(ctx, []string{"a"}, 99)
		if err != nil {
			t.Fatalf("traverse: %v", err)
		}
		if len(edges) != 2 {
			t.Errorf("got %d edges, want 2 (clamped to MaxGraphHops=%d)", len(edges), MaxGraphHops)
		}
		if containsString(reached, "d") {
			t.Error("clamping failed: traversal reached beyond MaxGraphHops")
		}
	})

	t.Run("traversal is undirected", func(t *testing.T) {
		// Seeding from the far end must find the same chain backwards.
		_, reached, err := g.Traverse(ctx, []string{"c"}, 1)
		if err != nil {
			t.Fatalf("traverse: %v", err)
		}
		if !containsString(reached, "b") || !containsString(reached, "d") {
			t.Errorf("reached %v, want both neighbours of c", reached)
		}
	})

	t.Run("empty seeds return nothing", func(t *testing.T) {
		edges, _, err := g.Traverse(ctx, nil, 2)
		if err != nil {
			t.Fatalf("traverse: %v", err)
		}
		if len(edges) != 0 {
			t.Errorf("got %d edges from empty seed set", len(edges))
		}
	})
}

// TestGraphStoreTraversalNoCycleBlowup ensures a cyclic graph terminates.
func TestGraphStoreTraversalNoCycleBlowup(t *testing.T) {
	g, _ := newEnabledGraph(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		mustUpsertEntity(t, g, id, "Node"+strings.ToUpper(id), EntityTypeConcept, 0.9)
	}
	mustUpsertEdge(t, g, "a", RelationRelatesTo, "b", "doc1", 0.9)
	mustUpsertEdge(t, g, "b", RelationRelatesTo, "c", "doc1", 0.9)
	mustUpsertEdge(t, g, "c", RelationRelatesTo, "a", "doc1", 0.9)

	edges, _, err := g.Traverse(ctx, []string{"a"}, MaxGraphHops)
	if err != nil {
		t.Fatalf("traverse cycle: %v", err)
	}
	if len(edges) > 3 {
		t.Errorf("cycle produced %d edges, want at most 3 (each edge once)", len(edges))
	}
}

func TestGraphStoreStats(t *testing.T) {
	g, _ := newEnabledGraph(t)
	ctx := context.Background()

	mustUpsertEntity(t, g, "a", "Alpha", EntityTypeConcept, 0.9)
	mustUpsertEntity(t, g, "b", "Beta", EntityTypeTechnology, 0.9)
	mustUpsertEdge(t, g, "a", RelationRelatesTo, "b", "doc1", 0.9)

	stats, err := g.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalEntities != 2 {
		t.Errorf("entities = %d, want 2", stats.TotalEntities)
	}
	if stats.TotalRelations != 1 {
		t.Errorf("edges = %d, want 1", stats.TotalRelations)
	}
	if stats.EntitiesByType[EntityTypeTechnology] != 1 {
		t.Errorf("technology entities = %d, want 1", stats.EntitiesByType[EntityTypeTechnology])
	}
	if stats.RelationsByType[RelationRelatesTo] != 1 {
		t.Errorf("relates_to edges = %d, want 1", stats.RelationsByType[RelationRelatesTo])
	}
}

// TestGraphStoreEnsureSchemaIdempotent guards the enabled-startup path.
func TestGraphStoreEnsureSchemaIdempotent(t *testing.T) {
	g, _ := newEnabledGraph(t)
	for i := 0; i < 3; i++ {
		if err := g.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("EnsureSchema call %d: %v", i+2, err)
		}
	}
}

// TestKAGSearcherUsesGraphWhenEnabled checks the wiring between the searcher and
// the edge tables, including the fallback to legacy kb_relations when off.
func TestKAGSearcherUsesGraphWhenEnabled(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO kb_entities (entity_id, name, type, description, confidence, source_document_id)
		VALUES ('ent1', 'Kubernetes', 'technology', 'Container orchestration', 0.95, 'doc1'),
		       ('ent2', 'Docker', 'technology', 'Container runtime', 0.9, 'doc1')
	`)
	if err != nil {
		t.Fatalf("seed entities: %v", err)
	}

	// A legacy relation that only the disabled path can see.
	if _, err := db.Exec(`
		INSERT INTO kb_relations (relation_id, subject_id, predicate, object_id, confidence)
		VALUES ('rel1', 'ent1', 'legacy_predicate', 'ent2', 0.9)
	`); err != nil {
		t.Fatalf("seed legacy relation: %v", err)
	}

	ctx := context.Background()

	t.Run("disabled graph falls back to kb_relations", func(t *testing.T) {
		s := NewKAGSearcher(db, NewGraphStore(db, false))
		if s.GraphEnabled() {
			t.Fatal("searcher reports graph enabled with a disabled store")
		}
		res, err := s.Search(ctx, &KAGSearchRequest{Query: "Kubernetes", IncludeRelations: true})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res.Relations) != 1 || res.Relations[0].Predicate != "legacy_predicate" {
			t.Errorf("relations = %+v, want the legacy row", res.Relations)
		}
	})

	t.Run("enabled graph traverses edges", func(t *testing.T) {
		g := NewGraphStore(db, true)
		if err := g.EnsureSchema(ctx); err != nil {
			t.Fatalf("ensure schema: %v", err)
		}
		mustUpsertEdge(t, g, "ent1", RelationDependsOn, "ent2", "doc1", 0.9)

		s := NewKAGSearcher(db, g)
		if !s.GraphEnabled() {
			t.Fatal("searcher reports graph disabled with an enabled store")
		}
		res, err := s.Search(ctx, &KAGSearchRequest{Query: "Kubernetes", IncludeRelations: true})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res.Relations) == 0 {
			t.Fatal("no relations from the enabled graph")
		}
		for _, r := range res.Relations {
			if r.Predicate == "legacy_predicate" {
				t.Error("enabled graph returned a legacy kb_relations row")
			}
		}
		if res.Relations[0].Predicate != string(RelationDependsOn) {
			t.Errorf("predicate = %q, want %q", res.Relations[0].Predicate, RelationDependsOn)
		}
		if res.Relations[0].SubjectName != "Kubernetes" || res.Relations[0].ObjectName != "Docker" {
			t.Errorf("endpoints = %s -> %s, want Kubernetes -> Docker",
				res.Relations[0].SubjectName, res.Relations[0].ObjectName)
		}
	})
}

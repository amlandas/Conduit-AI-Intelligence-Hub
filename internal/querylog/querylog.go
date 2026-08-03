// Package querylog records the *shape* of knowledge base queries -- never their
// content -- to a local append-only JSONL file.
//
// Why this exists: the July 2026 audit put the knowledge graph behind an
// evidence gate. Rich graph work is only worth re-opening if real usage shows
// real multi-hop demand, and nobody could answer that question because nothing
// was measured. This package measures it, at the smallest possible privacy cost.
//
// Privacy contract, enforced by construction:
//
//   - The Record struct has no field that can hold query text, entity names,
//     document titles, paths, snippets, or results. Not "redacted at write
//     time" -- structurally absent. The only way to leak a query through this
//     writer is to add a field, which is exactly what the redaction test
//     guards against.
//   - Nothing here opens a socket. The file is local, under the Conduit data
//     directory, and is never read back by Conduit itself.
//   - The writer is opt-out (telemetry.local_query_log). A disabled writer is
//     inert: it creates no file and does no I/O.
//
// "Local-only" is meant literally: this is a notebook the owner keeps about
// their own usage, not analytics.
package querylog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// FileName is the log's basename inside the Conduit data directory.
const FileName = "query-shape.jsonl"

// Record is one logged query shape.
//
// Every field is a count, a flag, or a fixed vocabulary string. There is
// deliberately no field for the query itself.
type Record struct {
	// Timestamp is when the query was served, RFC3339 with second precision.
	// Sub-second precision is omitted: it narrows nothing useful and makes
	// correlation with other logs easier than it should be.
	Timestamp string `json:"ts"`

	// Tool is the MCP tool name, e.g. "kag_query" or "kb_search".
	Tool string `json:"tool"`

	// TokenCount is how many whitespace-separated tokens the query had.
	TokenCount int `json:"token_count"`

	// HasEntityPattern reports whether the query looks like it names a specific
	// thing -- a capitalized proper noun, an ALLCAPS acronym, a CamelCase or
	// dotted identifier. Entity-shaped queries are the ones a graph could
	// plausibly serve better than plain retrieval.
	HasEntityPattern bool `json:"has_entity_pattern"`

	// HopDepth is the traversal depth the caller asked for, 0 when the tool
	// takes no hop argument. This is the number the evidence gate turns on:
	// if it is never above 1, multi-hop demand does not exist.
	HopDepth int `json:"hop_depth"`

	// GraphEnabled records whether the knowledge graph was on when this query
	// ran, so demand measured while the graph was off is not mistaken for
	// demand measured while it was on.
	GraphEnabled bool `json:"graph_enabled"`
}

// Writer appends Records to a JSONL file.
//
// The zero value is a disabled writer and is safe to use: every method is a
// no-op. Use New to get an enabled one.
type Writer struct {
	mu   sync.Mutex
	path string
	file *os.File
	// enabled is fixed at construction; a disabled writer never touches disk.
	enabled bool
}

// New creates a query-shape writer.
//
// enabled mirrors telemetry.local_query_log. When it is false, or dataDir is
// empty, the returned writer is inert and no file is created. The file itself is
// created lazily on the first successful Log call, with 0600 permissions.
func New(dataDir string, enabled bool) *Writer {
	if !enabled || dataDir == "" {
		return &Writer{}
	}
	return &Writer{
		path:    filepath.Join(dataDir, FileName),
		enabled: true,
	}
}

// Enabled reports whether this writer records anything.
func (w *Writer) Enabled() bool {
	return w != nil && w.enabled
}

// Path returns the log file path, or "" when disabled.
func (w *Writer) Path() string {
	if !w.Enabled() {
		return ""
	}
	return w.path
}

// Log appends one record.
//
// Failures are swallowed: a query must never fail because a local notebook could
// not be written. The timestamp is filled in if the caller left it empty.
func (w *Writer) Log(rec Record) {
	if !w.Enabled() {
		return
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
			return
		}
		f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		w.file = f
	}

	_, _ = w.file.Write(append(line, '\n'))
}

// Close releases the file handle.
func (w *Writer) Close() error {
	if !w.Enabled() {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// entityPatternRe matches shapes that suggest the query names a specific thing:
// an ALLCAPS acronym, a CamelCase identifier, or a dotted/slashed/underscored
// symbol. Plain capitalized words are handled separately so a sentence-initial
// capital does not count.
var entityPatternRe = regexp.MustCompile(`\b(?:[A-Z]{2,}[0-9]*|[a-z]+[A-Z][A-Za-z0-9]*|[A-Za-z][A-Za-z0-9]*(?:[._/][A-Za-z][A-Za-z0-9]*)+)\b`)

// Shape derives the loggable features of a query.
//
// The query string is read here and thrown away; it never reaches a Record.
// Callers pass the raw query so the feature extraction stays in one auditable
// place rather than being reimplemented per call site.
func Shape(tool, query string, hopDepth int, graphEnabled bool) Record {
	tokens := strings.Fields(query)

	return Record{
		Tool:             tool,
		TokenCount:       len(tokens),
		HasEntityPattern: hasEntityPattern(query, tokens),
		HopDepth:         hopDepth,
		GraphEnabled:     graphEnabled,
	}
}

// hasEntityPattern reports whether the query looks like it names something.
func hasEntityPattern(query string, tokens []string) bool {
	if entityPatternRe.MatchString(query) {
		return true
	}
	// A capitalized token that is not the first token reads as a proper noun.
	for i, tok := range tokens {
		if i == 0 {
			continue
		}
		r := []rune(tok)
		if len(r) > 1 && unicode.IsUpper(r[0]) {
			return true
		}
	}
	return false
}

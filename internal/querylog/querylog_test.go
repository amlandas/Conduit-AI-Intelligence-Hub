package querylog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// readLines returns the JSONL records written to the log.
func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line is not valid JSON: %q: %v", line, err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log: %v", err)
	}
	return out
}

func TestWriterDisabledWritesNothing(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, false)

	if w.Enabled() {
		t.Fatal("writer constructed with enabled=false reports enabled")
	}
	if w.Path() != "" {
		t.Errorf("disabled writer reports path %q", w.Path())
	}

	w.Log(Shape("kag_query", "anything at all", 2, true))
	if err := w.Close(); err != nil {
		t.Errorf("close: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("disabled writer created %d files in the data dir", len(entries))
	}
}

func TestWriterEmptyDataDirIsDisabled(t *testing.T) {
	w := New("", true)
	if w.Enabled() {
		t.Error("writer with no data dir reports enabled")
	}
	w.Log(Shape("kb_search", "query", 0, false)) // must not panic
}

func TestZeroValueWriterIsSafe(t *testing.T) {
	var w Writer
	w.Log(Shape("kb_search", "query", 0, false))
	if err := w.Close(); err != nil {
		t.Errorf("close zero writer: %v", err)
	}

	var nilW *Writer
	nilW.Log(Shape("kb_search", "query", 0, false))
	if nilW.Enabled() {
		t.Error("nil writer reports enabled")
	}
}

func TestWriterAppendsRecords(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, true)
	defer w.Close()

	w.Log(Shape("kag_query", "how does auth work", 2, true))
	w.Log(Shape("kb_search", "auth", 0, false))

	records := readLines(t, filepath.Join(dir, FileName))
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	if records[0]["tool"] != "kag_query" {
		t.Errorf("tool = %v, want kag_query", records[0]["tool"])
	}
	if records[0]["hop_depth"] != float64(2) {
		t.Errorf("hop_depth = %v, want 2", records[0]["hop_depth"])
	}
	if records[0]["graph_enabled"] != true {
		t.Errorf("graph_enabled = %v, want true", records[0]["graph_enabled"])
	}
	if records[1]["tool"] != "kb_search" {
		t.Errorf("tool = %v, want kb_search", records[1]["tool"])
	}
	if records[0]["ts"] == "" || records[0]["ts"] == nil {
		t.Error("timestamp missing")
	}
}

func TestWriterAppendsAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	w1 := New(dir, true)
	w1.Log(Shape("kb_search", "first", 0, false))
	w1.Close()

	w2 := New(dir, true)
	w2.Log(Shape("kb_search", "second", 0, false))
	w2.Close()

	records := readLines(t, filepath.Join(dir, FileName))
	if len(records) != 2 {
		t.Errorf("got %d records after reopen, want 2 (log must be append-only)", len(records))
	}
}

func TestWriterFilePermissions(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, true)
	defer w.Close()
	w.Log(Shape("kb_search", "query", 0, false))

	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log permissions = %o, want 600 (owner only)", perm)
	}
}

// TestRecordCannotCarryQueryText is the structural half of the privacy
// guarantee: no field on Record may hold free text from the user.
func TestRecordCannotCarryQueryText(t *testing.T) {
	allowed := map[string]bool{
		"Timestamp":        true,
		"Tool":             true,
		"TokenCount":       true,
		"HasEntityPattern": true,
		"HopDepth":         true,
		"GraphEnabled":     true,
	}

	rt := reflect.TypeOf(Record{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !allowed[f.Name] {
			t.Errorf("Record gained field %q (%s). Every new field is a potential "+
				"privacy leak: add it to the allowlist only after confirming it "+
				"cannot carry query text, entity names, paths, or results.",
				f.Name, f.Type)
		}
	}
}

// TestNoQueryTextReachesTheFile is the behavioral half: write queries full of
// distinctive, sensitive-looking strings and prove none of them land on disk.
func TestNoQueryTextReachesTheFile(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, true)
	defer w.Close()

	secrets := []string{
		"ProjectVoyager",
		"acquisition-of-Northwind",
		"patient_ssn_123456789",
		"api.internal.example.com/v2/billing",
		"Dr Amelia Hoffstadter",
		"/Users/someone/Documents/salaries.xlsx",
	}

	for _, secret := range secrets {
		w.Log(Shape("kag_query", "what do we know about "+secret+" and why", 2, true))
		w.Log(Shape("kb_search", secret, 0, false))
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	content := string(raw)

	for _, secret := range secrets {
		if strings.Contains(content, secret) {
			t.Errorf("query text %q leaked into the log file", secret)
		}
		// Also check the individual words, in case a field stored a fragment.
		for _, word := range strings.FieldsFunc(secret, func(r rune) bool {
			return r == ' ' || r == '-' || r == '_' || r == '/' || r == '.'
		}) {
			if len(word) < 5 {
				continue
			}
			if strings.Contains(content, word) {
				t.Errorf("query fragment %q leaked into the log file", word)
			}
		}
	}

	// Sanity: the file is not empty, i.e. the test above is not vacuous.
	records := readLines(t, filepath.Join(dir, FileName))
	if len(records) != len(secrets)*2 {
		t.Fatalf("got %d records, want %d -- redaction check would be vacuous",
			len(records), len(secrets)*2)
	}
}

func TestShapeFeatures(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		wantTokens   int
		wantEntityRe bool
	}{
		{"empty", "", 0, false},
		{"plain prose", "how does the login flow work", 6, false},
		{"acronym", "what is TLS", 3, true},
		{"camel case", "the parseRequest helper", 3, true},
		{"dotted symbol", "kb.kag.enabled default", 2, true},
		{"proper noun mid-sentence", "who maintains Kubernetes", 3, true},
		{"leading capital only", "Login flow", 2, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := Shape("kb_search", tc.query, 0, false)
			if rec.TokenCount != tc.wantTokens {
				t.Errorf("token count = %d, want %d", rec.TokenCount, tc.wantTokens)
			}
			if rec.HasEntityPattern != tc.wantEntityRe {
				t.Errorf("has_entity_pattern = %v, want %v", rec.HasEntityPattern, tc.wantEntityRe)
			}
		})
	}
}

func TestShapeRecordsHopDepth(t *testing.T) {
	if got := Shape("kag_query", "q", 3, true).HopDepth; got != 3 {
		t.Errorf("hop depth = %d, want 3", got)
	}
	if got := Shape("kb_search", "q", 0, true).HopDepth; got != 0 {
		t.Errorf("hop depth = %d, want 0 for a tool with no hop argument", got)
	}
}

// TestWriterConcurrentLogs runs under -race to prove the writer is safe for the
// concurrent tool calls an MCP session makes.
func TestWriterConcurrentLogs(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, true)
	defer w.Close()

	const goroutines, perGoroutine = 8, 25
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				w.Log(Shape("kb_search", "concurrent query text", 0, false))
			}
		}()
	}
	wg.Wait()

	records := readLines(t, filepath.Join(dir, FileName))
	if len(records) != goroutines*perGoroutine {
		t.Errorf("got %d records, want %d -- concurrent writes were lost or interleaved",
			len(records), goroutines*perGoroutine)
	}
}

// TestWriterSurvivesUnwritableDir proves a broken log never breaks a query.
func TestWriterSurvivesUnwritableDir(t *testing.T) {
	parent := t.TempDir()
	// A file where the writer expects a directory: MkdirAll and OpenFile both
	// fail, and Log must still return normally.
	blocked := filepath.Join(parent, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	w := New(filepath.Join(blocked, "sub"), true)
	defer w.Close()
	w.Log(Shape("kb_search", "query", 0, false))
	w.Log(Shape("kb_search", "query", 0, false))
}

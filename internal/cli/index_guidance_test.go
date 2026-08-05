package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// Regression tests for GitHub issue #97 on the CLI side.
//
// A user ran `conduit kb add` twice, never ran `conduit kb sync`, searched, and
// got a bare "No results found for: ...". Everything they had asked Conduit to
// index was sitting on disk, unindexed, and nothing said so.

// TestIssue97_UnsyncedKBNamesTheSyncCommand walks the reported sequence.
func TestIssue97_UnsyncedKBNamesTheSyncCommand(t *testing.T) {
	env := newTestEnv(t)

	env.mustRun(t, "kb", "add", corpus(t), "--name", "project-docs")
	env.mustRun(t, "kb", "add", t.TempDir(), "--name", "meeting-notes")
	// Deliberately NO `kb sync`. That is the bug.

	out, code := env.run(t, "kb", "search", "authentication")
	if code != 0 {
		t.Fatalf("searching an unsynced knowledge base should not fail, exited %d\n%s", code, out)
	}

	if !strings.Contains(out, "No results found for: authentication") {
		t.Fatalf("the existing empty-result line disappeared:\n%s", out)
	}
	if !strings.Contains(out, "conduit kb sync") {
		t.Errorf("the user is not told to run `conduit kb sync`:\n%s", out)
	}
	if !strings.Contains(out, "never been synced") {
		t.Errorf("the reason is not stated:\n%s", out)
	}
	for _, name := range []string{"project-docs", "meeting-notes"} {
		if !strings.Contains(out, name) {
			t.Errorf("source %q is not named:\n%s", name, out)
		}
	}
}

// TestIssue97_SyncedButEmptyKeepsThePlainMessage is the boundary.
//
// A knowledge base that has been indexed and simply does not contain the query
// must keep the bare message. Advising a sync there would be wrong, and a note
// on every empty search is noise -- which is how a real warning gets ignored.
func TestIssue97_SyncedButEmptyKeepsThePlainMessage(t *testing.T) {
	env := newTestEnv(t)

	env.mustRun(t, "kb", "add", corpus(t), "--name", "project-docs")
	env.mustRun(t, "kb", "sync")

	out, code := env.run(t, "kb", "search", "zzzznonexistentterm")
	if code != 0 {
		t.Fatalf("exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "No results found for: zzzznonexistentterm") {
		t.Fatalf("expected the empty-result message:\n%s", out)
	}
	if strings.Contains(out, "conduit kb sync") {
		t.Errorf("advised syncing a knowledge base that is already indexed:\n%s", out)
	}
	if strings.Contains(out, "never been synced") {
		t.Errorf("reported a sync gap that does not exist:\n%s", out)
	}
}

// TestIssue97_NoSourcesAtAllSaysSo covers the very first run.
func TestIssue97_NoSourcesAtAllSaysSo(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "kb", "search", "anything")
	if code != 0 {
		t.Fatalf("exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "conduit kb add") {
		t.Errorf("an empty knowledge base does not say how to fill it:\n%s", out)
	}
}

// TestIssue97_GuidanceReachesJSONConsumers.
//
// --json is an API contract consumed by scripts and by the frozen desktop GUI.
// The note is an ADDED key, index_note, present only when there is something to
// say; every existing key keeps its name, nesting and type.
func TestIssue97_GuidanceReachesJSONConsumers(t *testing.T) {
	env := newTestEnv(t)
	env.mustRun(t, "kb", "add", corpus(t), "--name", "project-docs")

	out := env.mustRun(t, "kb", "search", "authentication", "--json")

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse --json output: %v\n%s", err, out)
	}

	// Existing keys must still be there.
	for _, key := range []string{"results", "total_hits", "query", "search_mode"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("--json lost the %q key:\n%s", key, out)
		}
	}

	note, _ := payload["index_note"].(string)
	if !strings.Contains(note, "conduit kb sync") {
		t.Errorf("index_note missing or unhelpful: %q", note)
	}

	// And it must be absent once the knowledge base is indexed.
	env.mustRun(t, "kb", "sync")
	out = env.mustRun(t, "kb", "search", "zzzznonexistentterm", "--json")
	payload = nil
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse --json output: %v\n%s", err, out)
	}
	if _, present := payload["index_note"]; present {
		t.Errorf("index_note survived on a synced knowledge base:\n%s", out)
	}
}

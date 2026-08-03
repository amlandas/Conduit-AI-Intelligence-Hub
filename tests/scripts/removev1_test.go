package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v1Machine adds the leftovers a Conduit 1.x installer would have created.
func (e *env) v1Machine(t *testing.T) {
	t.Helper()
	e.writeFile(t, filepath.Join(e.home, ".local", "bin", "conduit-daemon"), "v1 daemon")
	e.writeFile(t, filepath.Join(e.dataDir, "daemon.log"), "daemon log contents")
	e.writeFile(t, filepath.Join(e.dataDir, "qdrant", "storage.dat"), "vectors")
	e.writeFile(t, filepath.Join(e.dataDir, "falkordb", "dump.rdb"), "graph")
	e.writeFile(t, filepath.Join(e.dataDir, "models", "model.gguf"), "weights")
}

// ---------------------------------------------------------------------------
// Dry run is the default, and it must be honest
// ---------------------------------------------------------------------------

// Nothing may be removed without --yes. This is the property the whole script
// is built around.
func TestRemoveV1DryRunIsTheDefault(t *testing.T) {
	e := newEnv(t)
	e.v1Machine(t)

	r := e.run(t, "remove-v1.sh")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}
	if !r.contains("DRY RUN") {
		t.Errorf("did not announce a dry run:\n%s", r.combined)
	}

	for _, path := range []string{
		filepath.Join(e.home, ".local", "bin", "conduit-daemon"),
		filepath.Join(e.dataDir, "daemon.log"),
		filepath.Join(e.dataDir, "qdrant", "storage.dat"),
		filepath.Join(e.dataDir, "conduit.db"),
	} {
		if !exists(path) {
			t.Errorf("the default run removed %s", path)
		}
	}
}

// A9: the help promises that nothing under the data directory is touched
// without --purge-data, and the summary prints "No user data was touched".
// daemon.log lives under the data directory, so removing it made both
// statements false. A teardown tool whose documented guarantees are only
// approximately true is worth less than one that removes slightly less.
func TestRemoveV1KeepsDaemonLogWithoutPurgeData(t *testing.T) {
	e := newEnv(t)
	e.v1Machine(t)
	log := filepath.Join(e.dataDir, "daemon.log")

	r := e.run(t, "remove-v1.sh", "--yes")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	if !exists(log) {
		t.Error("daemon.log was deleted although --purge-data was not given")
	}
	if !r.contains("No user data was touched") {
		t.Errorf("summary omits the no-user-data claim:\n%s", r.combined)
	}

	// Everything under the data directory must be intact.
	for _, path := range []string{
		filepath.Join(e.dataDir, "conduit.db"),
		filepath.Join(e.dataDir, "conduit.yaml"),
		filepath.Join(e.dataDir, "qdrant", "storage.dat"),
		filepath.Join(e.dataDir, "falkordb", "dump.rdb"),
		filepath.Join(e.dataDir, "models", "model.gguf"),
	} {
		if !exists(path) {
			t.Errorf("%s was removed without --purge-data", path)
		}
	}

	// The v1-only binary is outside the data directory and does go.
	if exists(filepath.Join(e.home, ".local", "bin", "conduit-daemon")) {
		t.Error("conduit-daemon survived a live run")
	}
}

// --purge-data removes the two dead container stores and the log, and still
// never touches the knowledge base or the models.
func TestRemoveV1PurgeDataScope(t *testing.T) {
	e := newEnv(t)
	e.v1Machine(t)

	r := e.run(t, "remove-v1.sh", "--yes", "--purge-data")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	for _, gone := range []string{
		filepath.Join(e.dataDir, "qdrant"),
		filepath.Join(e.dataDir, "falkordb"),
		filepath.Join(e.dataDir, "daemon.log"),
	} {
		if exists(gone) {
			t.Errorf("--purge-data did not remove %s", gone)
		}
	}

	// The knowledge base and the models are user data in every reading, and
	// --purge-data is documented as covering only the v1 container stores.
	for _, kept := range []string{
		filepath.Join(e.dataDir, "conduit.db"),
		filepath.Join(e.dataDir, "conduit.yaml"),
		filepath.Join(e.dataDir, "models", "model.gguf"),
	} {
		if !exists(kept) {
			t.Errorf("--purge-data destroyed %s, which it does not own", kept)
		}
	}
}

// The knowledge base survives a full live run. If only one assertion in this
// file survived, it should be this one.
func TestRemoveV1NeverTouchesTheKnowledgeBase(t *testing.T) {
	e := newEnv(t)
	e.v1Machine(t)

	db := filepath.Join(e.dataDir, "conduit.db")
	before := readFile(t, db)

	for _, args := range [][]string{
		{"--yes"},
		{"--yes", "--purge-data"},
		{"--dry-run"},
	} {
		if r := e.run(t, "remove-v1.sh", args...); r.code != 0 {
			t.Fatalf("remove-v1.sh %v exited %d\n%s", args, r.code, r.combined)
		}
		if !exists(db) {
			t.Fatalf("remove-v1.sh %v deleted the knowledge base", args)
		}
		if readFile(t, db) != before {
			t.Fatalf("remove-v1.sh %v modified the knowledge base", args)
		}
	}
}

// Running it twice must be safe and must not report phantom work.
func TestRemoveV1IsIdempotent(t *testing.T) {
	e := newEnv(t)
	e.v1Machine(t)

	if r := e.run(t, "remove-v1.sh", "--yes"); r.code != 0 {
		t.Fatalf("first run exited %d\n%s", r.code, r.combined)
	}
	r := e.run(t, "remove-v1.sh", "--yes")
	if r.code != 0 {
		t.Fatalf("second run exited %d\n%s", r.code, r.combined)
	}
	if strings.Contains(r.combined, "conduit-daemon") && r.contains("removed  ") {
		t.Errorf("second run claims to have removed something again:\n%s", r.combined)
	}
}

// A clean machine must say so rather than inventing findings.
func TestRemoveV1OnCleanMachine(t *testing.T) {
	e := newEnv(t)

	r := e.run(t, "remove-v1.sh", "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}
	if !r.contains("already clean") && !r.contains("0 removed") {
		t.Errorf("did not report a clean machine:\n%s", r.combined)
	}
}

// A symlinked data directory is refused here too. `rm -rf link/` would empty
// the target.
func TestRemoveV1RefusesSymlinkedDataDir(t *testing.T) {
	e := newEnv(t)

	realDir := filepath.Join(e.home, "elsewhere")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	precious := filepath.Join(realDir, "conduit.db")
	e.writeFile(t, precious, "the real knowledge base")

	link := filepath.Join(e.home, "linked")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	r := e.run(t, "remove-v1.sh", "--data-dir", link+"/", "--yes", "--purge-data")
	if r.code == 0 {
		t.Fatalf("accepted a symlinked data directory\n%s", r.combined)
	}
	if !exists(precious) {
		t.Fatal("the symlink target's contents were destroyed")
	}
}

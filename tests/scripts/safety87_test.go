package scripts

import (
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// #87(a) -- the real-home helper's own guard
// ---------------------------------------------------------------------------

// runWithRealHome is the one helper in this suite that points a deletion script
// at the developer's actual home directory. Its guard accepted anything that
// merely contained --dry-run, so `--remove-data --force --dry-run` was allowed
// through and the only thing preventing a real deletion was that --dry-run
// happened to still work. That is a single point of failure guarding somebody's
// knowledge base.
//
// The rule is tested here rather than by calling the helper, because the
// helper's response to a violation is t.Fatalf, which a test cannot observe.
func TestRealHomeGuardRejectsDestructiveFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string // a fragment the refusal must mention
	}{
		{"remove-data", []string{"--data-dir", "/x", "--remove-data", "--dry-run"}, "--remove-data"},
		{"force", []string{"--data-dir", "/x", "--force", "--dry-run"}, "--force"},
		{"force short", []string{"--data-dir", "/x", "-f", "--dry-run"}, "-f"},
		{"manual", []string{"--manual", "--dry-run"}, "--manual"},
		{"no dry-run", []string{"--data-dir", "/x"}, "--dry-run"},
		{"empty", nil, "--dry-run"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := assertRealHomeArgsSafe(c.args)
			if err == nil {
				t.Fatalf("accepted %v against the real home directory", c.args)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("refusal does not name %s: %v", c.want, err)
			}
		})
	}
}

// The guard must still allow what the case-folding tests actually need, or it
// has simply disabled them.
func TestRealHomeGuardAllowsAnInertDryRun(t *testing.T) {
	if err := assertRealHomeArgsSafe([]string{"--data-dir", "/SOMEWHERE", "--dry-run"}); err != nil {
		t.Fatalf("rejected the invocation the guard tests depend on: %v", err)
	}
}

// ---------------------------------------------------------------------------
// #87(b) -- /System/Volumes/Data
// ---------------------------------------------------------------------------

// On macOS Catalina and later the system volume is read-only and everything
// writable lives on a second volume mounted at /System/Volumes/Data. That
// directory is the root of every user's data: /System/Volumes/Data/Users/<you>
// is the same directory as /Users/<you>, reached through a firmlink.
//
// /System was already on the deny list, but the list is matched by exact path
// and by device:inode, and neither catches a child. /System/Volumes/Data is its
// own mount with its own inode, distinct from / and from /Users, so
// `--data-dir /System/Volumes/Data` sailed through every check and named the
// entire writable filesystem.
func TestGuardProtectsTheMacOSDataVolume(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			r := e.run(t, script, "--data-dir", "/System/Volumes/Data", "--dry-run")
			if r.code == 0 {
				t.Fatalf("%s accepted /System/Volumes/Data\n%s", script, r.combined)
			}
			if !r.contains("refusing") {
				t.Errorf("no guard message:\n%s", r.combined)
			}
		})
	}
}

// A trailing separator is the normal case, not an exotic one: tab completion
// appends it.
func TestGuardProtectsTheDataVolumeWithTrailingSlash(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			r := e.run(t, script, "--data-dir", "/System/Volumes/Data/", "--dry-run")
			if r.code == 0 {
				t.Fatalf("%s accepted /System/Volumes/Data/\n%s", script, r.combined)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #87(c) -- CONDUIT_DATA_DIR is user input too
// ---------------------------------------------------------------------------

// The absolute-path refusal was gated on DATA_DIR_EXPLICIT, which was only set
// by the --data-dir flag. A value arriving in CONDUIT_DATA_DIR skipped it
// entirely and was then silently made absolute against the working directory --
// the precise outcome the flag's guard exists to prevent, reached by the other
// door.
//
// The two are the same kind of thing: a directory the user named, as opposed to
// the default this script picked.
func TestEnvSuppliedDataDirMustBeAbsolute(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		for _, dir := range []string{"relative/path", "./conduit", "../conduit", "conduit"} {
			t.Run(script+" "+dir, func(t *testing.T) {
				e := newEnv(t)
				r := e.runWithEnv(t, []string{"CONDUIT_DATA_DIR=" + dir}, "", script, "--dry-run")
				if r.code == 0 {
					t.Fatalf("%s accepted CONDUIT_DATA_DIR=%q\n%s", script, dir, r.combined)
				}
				if !r.contains("absolute") {
					t.Errorf("the error does not explain the rule:\n%s", r.combined)
				}
			})
		}
	}
}

// The deny list must apply to the environment variable as well. Guarding only
// the flag would mean `CONDUIT_DATA_DIR=$HOME ./uninstall.sh --remove-data`
// was accepted.
func TestEnvSuppliedDataDirIsDenyListed(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			r := e.runWithEnv(t, []string{"CONDUIT_DATA_DIR=" + e.home}, "", script, "--dry-run")
			if r.code == 0 {
				t.Fatalf("%s accepted CONDUIT_DATA_DIR=$HOME\n%s", script, r.combined)
			}
			if !r.contains("refusing") {
				t.Errorf("no guard message:\n%s", r.combined)
			}
		})
	}
}

// A directory named in the environment must also reach the binary.
//
// The binary does not read CONDUIT_DATA_DIR -- only these scripts do. So when
// the script honoured the variable but did not forward --data-dir, delegation
// ran against the binary's own default: the user asked to remove the directory
// they had named and Conduit removed ~/.conduit instead. Guarding a path and
// then deleting a different one is worse than not guarding it.
func TestEnvSuppliedDataDirIsForwardedToTheBinary(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	named := filepath.Join(e.home, "elsewhere", "conduit-data")
	e.writeFile(t, filepath.Join(named, "conduit.db"), "pretend sqlite")

	r := e.runWithEnv(t, []string{"CONDUIT_DATA_DIR=" + named}, "", "uninstall.sh", "--dry-run")
	if r.code != 0 {
		t.Fatalf("uninstall exited %d\n%s", r.code, r.combined)
	}

	invocations := strings.Join(e.realInvocations(t), "\n")
	if !strings.Contains(invocations, "--data-dir "+named) {
		t.Errorf("the binary was not told which directory the user named.\ninvocations:\n%s\noutput:\n%s",
			invocations, r.combined)
	}
}

// The default is not user input and must not be caught by the absolute-path
// rule -- the same property TestGuardAllowsDefaultDataDir asserts for the flag,
// restated for the environment so that a fix to one cannot break the other.
func TestUnsetEnvStillAllowsTheDefault(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			if script == "uninstall.sh" {
				e.installStubOnPath(t, v2Stub())
			}
			// CONDUIT_DATA_DIR deliberately absent.
			r := e.run(t, script, "--dry-run")
			if r.code != 0 {
				t.Fatalf("%s rejected its own default data directory\n%s", script, r.combined)
			}
		})
	}
}

// An empty CONDUIT_DATA_DIR is the shape a mistyped export leaves behind
// (`export CONDUIT_DATA_DIR=`). It must fall back to the default rather than
// resolve to the working directory.
func TestEmptyEnvDataDirFallsBackToTheDefault(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			if script == "uninstall.sh" {
				e.installStubOnPath(t, v2Stub())
			}
			r := e.runWithEnv(t, []string{"CONDUIT_DATA_DIR="}, "", script, "--dry-run")
			if r.code != 0 {
				t.Fatalf("%s failed with an empty CONDUIT_DATA_DIR\n%s", script, r.combined)
			}
		})
	}
}

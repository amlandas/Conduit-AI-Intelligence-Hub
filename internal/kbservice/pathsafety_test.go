package kbservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/kb"
)

// TestPathIsWithin covers the containment predicate the path-safety check is
// built on. The interesting cases are the ones a naive strings.HasPrefix gets
// wrong.
func TestPathIsWithin(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		parent string
		want   bool
	}{
		{name: "identical", path: "/etc", parent: "/etc", want: true},
		{name: "direct child", path: "/etc/hosts", parent: "/etc", want: true},
		{name: "deep descendant", path: "/etc/a/b/c", parent: "/etc", want: true},
		{
			name: "sibling with a shared prefix is NOT within",
			path: "/etcetera", parent: "/etc", want: false,
		},
		{
			name: "the prefix must end on a separator",
			path: "/home/userdata", parent: "/home/user", want: false,
		},
		{
			// A root entry means "do not index the whole machine", not "refuse
			// every path there is".
			name: "the filesystem root matches only itself",
			path: "/anywhere/at/all", parent: "/", want: false,
		},
		{name: "the filesystem root matches itself", path: "/", parent: "/", want: true},
		{name: "unrelated", path: "/opt/data", parent: "/etc", want: false},
		{name: "traversal is cleaned away", path: "/etc/../opt", parent: "/etc", want: false},
		{name: "traversal back in is caught", path: "/opt/../etc/ssh", parent: "/etc", want: true},
		{name: "empty parent matches nothing", path: "/etc", parent: "", want: false},
		{name: "blank parent matches nothing", path: "/etc", parent: "   ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("POSIX path fixtures")
			}
			if got := pathIsWithin(tt.path, tt.parent); got != tt.want {
				t.Errorf("pathIsWithin(%q, %q) = %v, want %v", tt.path, tt.parent, got, tt.want)
			}
		})
	}
}

// TestCheckSourcePath covers the config-driven decision.
func TestCheckSourcePath(t *testing.T) {
	cfg := testConfig(t)
	home := t.TempDir()

	secrets := filepath.Join(home, ".ssh")
	docs := filepath.Join(home, "Documents")
	notes := filepath.Join(home, "notes")
	for _, d := range []string{secrets, docs, notes, filepath.Join(secrets, "keys")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// config.Load expands "~" before anything reads these; the test supplies
	// already-expanded values, as a loaded config would.
	cfg.Policy.ForbiddenPaths = []string{secrets}
	cfg.Policy.WarnPaths = []string{docs}

	t.Run("a forbidden path is refused", func(t *testing.T) {
		_, err := checkSourcePath(cfg, secrets)
		var forbidden *ErrPathForbidden
		if !errors.As(err, &forbidden) {
			t.Fatalf("err = %v, want *ErrPathForbidden", err)
		}
		if forbidden.Forbidden != secrets {
			t.Errorf("Forbidden = %q, want %q", forbidden.Forbidden, secrets)
		}
		if !strings.Contains(err.Error(), "forbidden_paths") {
			t.Errorf("the error does not tell the user where the rule came from: %v", err)
		}
	})

	t.Run("a directory beneath a forbidden path is refused", func(t *testing.T) {
		_, err := checkSourcePath(cfg, filepath.Join(secrets, "keys"))
		var forbidden *ErrPathForbidden
		if !errors.As(err, &forbidden) {
			t.Fatalf("err = %v, want *ErrPathForbidden", err)
		}
	})

	t.Run("a symlink into a forbidden path is refused", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs privileges on Windows")
		}
		link := filepath.Join(home, "innocuous")
		if err := os.Symlink(secrets, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		_, err := checkSourcePath(cfg, link)
		var forbidden *ErrPathForbidden
		if !errors.As(err, &forbidden) {
			t.Fatalf("err = %v, want *ErrPathForbidden -- a symlink is a way around a lexical check", err)
		}
	})

	t.Run("a warn path is allowed with a warning", func(t *testing.T) {
		warnings, err := checkSourcePath(cfg, docs)
		if err != nil {
			t.Fatalf("a warn path must not be refused: %v", err)
		}
		if len(warnings) != 1 {
			t.Fatalf("warnings = %v, want exactly one", warnings)
		}
		if !strings.Contains(warnings[0], "warn_paths") {
			t.Errorf("the warning does not say where the rule came from: %q", warnings[0])
		}
	})

	t.Run("an ordinary path is allowed silently", func(t *testing.T) {
		warnings, err := checkSourcePath(cfg, notes)
		if err != nil {
			t.Fatalf("checkSourcePath: %v", err)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings = %v, want none", warnings)
		}
	})

	t.Run("a nil config allows everything", func(t *testing.T) {
		if _, err := checkSourcePath(nil, secrets); err != nil {
			t.Errorf("checkSourcePath(nil, ...) = %v, want nil", err)
		}
	})
}

// TestAddSourceEnforcesPathSafety is the end-to-end half: the check is wired
// into the operation, not merely available.
//
// Before WP-3.4 config.PolicyConfig was declared, defaulted, expanded on load,
// printed by `conduit config` -- and read by nothing. `conduit kb add ~/.ssh`
// succeeded and indexed every private key into a database the MCP server serves
// to AI clients on request.
func TestAddSourceEnforcesPathSafety(t *testing.T) {
	cfg := testConfig(t)
	home := t.TempDir()

	secrets := filepath.Join(home, ".ssh")
	docs := filepath.Join(home, "Documents")
	for _, d := range []string{secrets, docs} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	cfg.Policy.ForbiddenPaths = []string{secrets}
	cfg.Policy.WarnPaths = []string{docs}

	svc := openTestService(t, cfg)
	ctx := context.Background()

	t.Run("forbidden", func(t *testing.T) {
		_, err := svc.AddSource(ctx, kb.AddSourceRequest{Path: secrets, Name: "keys"})
		var forbidden *ErrPathForbidden
		if !errors.As(err, &forbidden) {
			t.Fatalf("AddSource(%s) = %v, want *ErrPathForbidden", secrets, err)
		}

		// And nothing was written.
		list, err := svc.ListSources(ctx)
		if err != nil {
			t.Fatalf("ListSources: %v", err)
		}
		for _, s := range list.Sources {
			if s.Path == secrets {
				t.Errorf("the refused source was registered anyway: %+v", s)
			}
		}
	})

	t.Run("warned but allowed", func(t *testing.T) {
		src, warnings, err := svc.AddSourceWithWarnings(ctx, kb.AddSourceRequest{Path: docs, Name: "docs"})
		if err != nil {
			t.Fatalf("AddSourceWithWarnings: %v", err)
		}
		if src == nil {
			t.Fatal("nil source")
		}
		if len(warnings) == 0 {
			t.Error("a warn path produced no warning")
		}
	})

	t.Run("the default config forbids the usual credential directories", func(t *testing.T) {
		// The shipped defaults are the actual protection; a test that only ever
		// uses substituted lists would not notice them being emptied.
		def := testConfig(t)
		var sawSSH bool
		for _, p := range def.Policy.ForbiddenPaths {
			if strings.HasSuffix(p, string(filepath.Separator)+".ssh") || p == "~/.ssh" {
				sawSSH = true
			}
		}
		if !sawSSH {
			t.Errorf("the default forbidden_paths no longer covers ~/.ssh: %v", def.Policy.ForbiddenPaths)
		}
	})
}

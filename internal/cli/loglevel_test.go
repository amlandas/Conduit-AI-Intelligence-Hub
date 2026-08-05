package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/observability"
)

// --log-level is a documented persistent flag, and for the whole of v2 so far it
// did nothing at all: observability.SetupLogging had no caller outside a test,
// so zerolog's global level stayed at its zero value and every command wrote
// debug JSON to stderr no matter what the user asked for. That is what put raw
// log lines through the middle of `install.sh`'s transcript.
//
// This asserts through the real path -- NewRootCommand, its PersistentPreRun,
// applyLogLevel -- so it fails if the hook is removed, renamed, or moved
// somewhere a command can bypass. It was moved once already: it started in
// loadConfig, which `conduit doctor` does not call (runDoctor uses
// config.LoadWithFlags directly), so `doctor --log-level error` still printed
// debug lines.
func TestLogLevelFlagIsApplied(t *testing.T) {
	newTestEnv(t)

	// The global level is process-wide state shared with every other test in
	// this package.
	original := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(original) })

	cases := []struct {
		flag string
		want zerolog.Level
	}{
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
	}

	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			// Deliberately set to something else first, so a passing result
			// cannot come from the level already happening to be right.
			zerolog.SetGlobalLevel(zerolog.TraceLevel)

			root := NewRootCommand()
			root.SetArgs([]string{"--log-level", tc.flag, "version"})
			if err := root.Execute(); err != nil {
				t.Fatalf("version --log-level %s: %v", tc.flag, err)
			}

			if got := zerolog.GlobalLevel(); got != tc.want {
				t.Errorf("--log-level %s left the global level at %v, want %v; "+
					"the flag is documented and must do something",
					tc.flag, got, tc.want)
			}
		})
	}
}

// Every command has to get the treatment, not just the ones that happen to call
// loadConfig. doctor is the one that did not.
func TestLogLevelAppliesToEveryCommand(t *testing.T) {
	e := newTestEnv(t)

	original := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(original) })

	for _, args := range [][]string{
		{"version"},
		{"doctor"},
		{"kb", "stats"},
		{"mcp", "status"},
		{"config", "show"},
	} {
		t.Run(args[0]+"_"+lastArg(args), func(t *testing.T) {
			zerolog.SetGlobalLevel(zerolog.TraceLevel)

			// Exit status is not the subject: doctor legitimately fails on a
			// machine with no embedding model.
			_, _ = e.run(t, append([]string{"--log-level", "error"}, args...)...)

			if got := zerolog.GlobalLevel(); got != zerolog.ErrorLevel {
				t.Errorf("`conduit %v --log-level error` left the level at %v; "+
					"this command reaches its configuration by a route that "+
					"skips the flag", args, got)
			}
		})
	}
}

func lastArg(args []string) string {
	return args[len(args)-1]
}

// ---------------------------------------------------------------------------
// Issue #98: the surviving lines have to be readable.
//
// #95 (the two tests above) made --log-level do something. It did not change
// what a line LOOKS like, so `conduit kb add` still printed
// {"level":"info","component":"kb.source",...} into its own transcript and
// `conduit kb search` still wrapped its llama-server guidance in JSON.
//
// The two tests above are unchanged: they assert on zerolog.GlobalLevel() only,
// and the level plumbing they pin is exactly as it was. What follows pins the
// FORMAT decision that now sits beside it.
// ---------------------------------------------------------------------------

// TestResolveLogFormat pins which context gets which format.
func TestResolveLogFormat(t *testing.T) {
	root := NewRootCommand()

	find := func(t *testing.T, path ...string) *cobra.Command {
		t.Helper()
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("find %v: %v", path, err)
		}
		return cmd
	}

	mcpKB := find(t, "mcp", "kb")
	kbSearch := find(t, "kb", "search")

	cases := []struct {
		name    string
		format  string
		level   string
		cmd     *cobra.Command
		want    string
		because string
	}{
		{
			name:   "interactive command at the default level is human-readable",
			format: config.LogFormatAuto, level: "warn", cmd: kbSearch,
			want:    observability.LogFormatConsole,
			because: "this is the case #98 was filed about",
		},
		{
			name:   "mcp kb is always JSON",
			format: config.LogFormatAuto, level: "warn", cmd: mcpKB,
			want: observability.LogFormatJSON,
			because: "its stderr is read by an AI client's process supervisor, " +
				"and TestMCPKBStdoutIsProtocolPureUnderDebugLogging depends on the stream staying machine-shaped",
		},
		{
			name:   "mcp kb is still JSON at debug",
			format: config.LogFormatAuto, level: "debug", cmd: mcpKB,
			want: observability.LogFormatJSON,
		},
		{
			name:   "debug is JSON even for an interactive command",
			format: config.LogFormatAuto, level: "debug", cmd: kbSearch,
			want:    observability.LogFormatJSON,
			because: "debug output exists to be pasted into a bug report",
		},
		{
			name:   "trace is JSON too",
			format: config.LogFormatAuto, level: "trace", cmd: kbSearch,
			want: observability.LogFormatJSON,
		},
		{
			name:   "an explicit json outranks the interactive default",
			format: observability.LogFormatJSON, level: "warn", cmd: kbSearch,
			want:    observability.LogFormatJSON,
			because: "someone who set log_format meant it",
		},
		{
			name:   "an explicit console outranks even mcp kb",
			format: observability.LogFormatConsole, level: "warn", cmd: mcpKB,
			want:    observability.LogFormatConsole,
			because: "same reason; stdout purity does not depend on stderr's shape",
		},
		{
			name:   "an empty format behaves as auto",
			format: "", level: "warn", cmd: kbSearch,
			want: observability.LogFormatConsole,
		},
		{
			name:   "a nil command (no subcommand resolved) is interactive",
			format: config.LogFormatAuto, level: "warn", cmd: nil,
			want: observability.LogFormatConsole,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := "<nil>"
			if tc.cmd != nil {
				path = tc.cmd.CommandPath()
			}
			if got := resolveLogFormat(tc.format, tc.level, tc.cmd); got != tc.want {
				t.Errorf("resolveLogFormat(%q, %q, %s) = %q, want %q\n%s",
					tc.format, tc.level, path, got, tc.want, tc.because)
			}
		})
	}
}

// TestInteractiveCommandsEmitNoRawJSON runs real commands through the real
// entry point and reads their stderr.
//
// This is the shape of the bug report: the user saw {"level":"info",...} lines
// while running ordinary commands. tests/scripts/installer_hardening_test.go
// asserts the same thing about install.sh's transcript; this asserts it about
// the binary directly, so a regression is caught without building a release.
func TestInteractiveCommandsEmitNoRawJSON(t *testing.T) {
	e := newTestEnv(t)

	original := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(original) })

	// newTestEnv sets embed.provider=none, which logs nothing at all and would
	// make this test pass without exercising anything. Point at llama-server
	// with a binary that cannot exist: that is the reporter's machine, and it
	// is what makes the "embedding provider unavailable" warning -- the line
	// that carried the llama-server guidance -- actually fire.
	t.Setenv("CONDUIT_EMBED_PROVIDER", "llama-server")
	t.Setenv("CONDUIT_EMBED_LLAMA_SERVER_BINARY",
		filepath.Join(t.TempDir(), "no-such-llama-server"))

	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "notes.md"),
		[]byte("# Notes\n\nAccess tokens expire after one hour.\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	sawGuidance := false

	for _, args := range [][]string{
		{"kb", "add", docs, "--name", "notes"},
		{"kb", "sync"},
		{"kb", "search", "how do tokens expire"},
		{"kb", "stats"},
		{"status"},
	} {
		t.Run(args[0]+"_"+lastArg(args[:min(2, len(args))]), func(t *testing.T) {
			out, stderr := runCapturingStderr(t, e, args...)
			for _, stream := range []struct{ name, body string }{
				{"stdout", out}, {"stderr", stderr},
			} {
				for _, line := range strings.Split(stream.body, "\n") {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, `{"level":`) {
						t.Errorf("raw log JSON on %s of `conduit %s`:\n%s",
							stream.name, strings.Join(args, " "), trimmed)
					}
				}
			}
			if strings.Contains(stderr, "conduit config set embed.provider none") {
				sawGuidance = true
				// The guidance is the point of keeping warnings on by default.
				// It has to be readable, which means human-formatted.
				if !strings.Contains(stderr, "warning:") {
					t.Errorf("guidance appeared without a human-readable severity:\n%s", stderr)
				}
				if strings.Contains(stderr, `\"`) {
					t.Errorf("guidance was rendered with JSON escaping:\n%s", stderr)
				}
			}
		})
	}

	if !sawGuidance {
		t.Error("no command surfaced the llama-server guidance; this test proved nothing. " +
			"It must remain visible at the DEFAULT log level -- that is the #98 requirement.")
	}
}

// TestDebugLevelStillEmitsJSON is the other direction: the structured stream
// must remain available, because that is what a bug report needs.
func TestDebugLevelStillEmitsJSON(t *testing.T) {
	e := newTestEnv(t)

	original := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(original) })

	_, stderr := runCapturingStderr(t, e, "--log-level", "debug", "kb", "stats")
	if !strings.Contains(stderr, `"level":"debug"`) {
		t.Errorf("--log-level debug produced no structured output on stderr:\n%s", stderr)
	}
}

// runCapturingStderr runs a CLI invocation and returns stdout and stderr
// separately. testEnv.run captures stdout only, and #98 is about stderr.
func runCapturingStderr(t *testing.T, e *testEnv, args ...string) (string, string) {
	t.Helper()

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	out, _ := e.run(t, args...)

	_ = w.Close()
	os.Stderr = origStderr
	stderr := <-done
	_ = r.Close()

	return out, stderr
}

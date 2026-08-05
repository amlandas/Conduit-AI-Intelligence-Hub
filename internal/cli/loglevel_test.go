package cli

import (
	"testing"

	"github.com/rs/zerolog"
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

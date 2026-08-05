package observability

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// restoreLogger puts the process-wide zerolog state back after a test.
func restoreLogger(t *testing.T) {
	t.Helper()
	origLogger := log.Logger
	origLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		log.Logger = origLogger
		zerolog.SetGlobalLevel(origLevel)
	})
}

// TestSetupLogging_ConsoleFormatIsHumanReadable is the #98 regression.
//
// Was: every caller went through SetupDefaultLogging, which hardcoded "json",
// so `conduit kb add` printed
//
//	{"level":"info","component":"kb.source",...,"caller":"..."}
//
// through the middle of its own transcript, and `conduit kb search` wrapped its
// llama-server installation guidance in the same JSON.
func TestSetupLogging_ConsoleFormatIsHumanReadable(t *testing.T) {
	restoreLogger(t)

	var buf bytes.Buffer
	SetupLogging("warn", LogFormatConsole, &buf)
	lg := Logger("kb.searcher")
	lg.Warn().
		Err(errors.New("llama-server not found: install it with `brew install llama.cpp`")).
		Str("document_id", "doc_1").
		Msg("semantic search failed, using FTS5 only")

	out := buf.String()

	if strings.HasPrefix(strings.TrimSpace(out), "{") || strings.Contains(out, `"level":`) {
		t.Fatalf("console format emitted JSON:\n%s", out)
	}
	if !strings.Contains(out, "warning:") {
		t.Errorf("severity is not human-readable:\n%s", out)
	}
	if !strings.Contains(out, "semantic search failed, using FTS5 only") {
		t.Errorf("message missing:\n%s", out)
	}

	// The remedy must survive intact and unescaped -- it is the whole reason
	// the line is shown at all.
	if !strings.Contains(out, "brew install llama.cpp") {
		t.Errorf("the error's remedy was lost:\n%s", out)
	}
	if strings.Contains(out, `\"`) || strings.Contains(out, "\\`") {
		t.Errorf("the error was rendered with escape sequences instead of prose:\n%s", out)
	}

	// Clutter a person cannot act on.
	if strings.Contains(out, "component=") || strings.Contains(out, "kb.searcher") {
		t.Errorf("console output leaks the component field:\n%s", out)
	}
	if strings.Contains(out, "logging_test.go") {
		t.Errorf("console output leaks the caller:\n%s", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("console output should be exactly one line:\n%q", out)
	}
}

// TestSetupLogging_JSONFormatKeepsTheStructuredFields is the other half:
// --log-level debug and `conduit mcp kb` must still get everything.
func TestSetupLogging_JSONFormatKeepsTheStructuredFields(t *testing.T) {
	restoreLogger(t)

	var buf bytes.Buffer
	SetupLogging("debug", LogFormatJSON, &buf)
	lg := Logger("kb.searcher")
	lg.Debug().Str("query", "lantern").Msg("search completed")

	out := buf.String()
	for _, want := range []string{
		`"level":"debug"`, `"component":"kb.searcher"`, `"query":"lantern"`,
		`"time":`, `"caller":`, `"message":"search completed"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output is missing %s:\n%s", want, out)
		}
	}
}

// TestSetupLogging_LevelIsHonouredInBothFormats guards against a format change
// quietly resetting the global level.
func TestSetupLogging_LevelIsHonouredInBothFormats(t *testing.T) {
	restoreLogger(t)

	for _, format := range []string{LogFormatConsole, LogFormatJSON} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			SetupLogging("warn", format, &buf)

			lg := Logger("x")
			lg.Info().Msg("narration")
			if buf.Len() != 0 {
				t.Errorf("info survived at level warn:\n%s", buf.String())
			}

			lg.Warn().Msg("this matters")
			if !strings.Contains(buf.String(), "this matters") {
				t.Errorf("warn was suppressed at level warn:\n%s", buf.String())
			}
		})
	}
}

// TestSetupLogging_NonTerminalOutputHasNoColour: an installer transcript, a CI
// log and a redirected file are all read as plain text.
func TestSetupLogging_NonTerminalOutputHasNoColour(t *testing.T) {
	restoreLogger(t)

	var buf bytes.Buffer
	SetupLogging("warn", LogFormatConsole, &buf)
	lg := Logger("x")
	lg.Warn().Msg("plain")

	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("ANSI escapes written to a non-terminal:\n%q", buf.String())
	}
}

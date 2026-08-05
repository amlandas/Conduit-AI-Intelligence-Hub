// Package observability provides logging, metrics, and tracing for Conduit.
package observability

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Log format identifiers accepted by SetupLogging and by the log_format
// configuration key.
const (
	// LogFormatConsole is human-readable one-line output: no timestamp, no
	// caller, no component field. It is what a person reading a terminal wants.
	LogFormatConsole = "console"

	// LogFormatText is an accepted alias for console.
	LogFormatText = "text"

	// LogFormatJSON is the structured stream. It is what a machine reading
	// stderr wants, and what belongs in a bug report.
	LogFormatJSON = "json"
)

// SetupLogging configures the global logger.
//
// Issue #98: a diagnostic is only useful if the person it is addressed to can
// read it. `conduit kb add` printed
//
//	{"level":"info","component":"kb.source","path":"/x","time":"...","caller":"..."}
//
// through the middle of its own transcript, and `conduit kb search` wrapped its
// llama-server installation guidance -- carefully written, genuinely useful --
// in the same JSON, where nobody reads it. Structured logging is right for a
// log aggregator and wrong for a terminal.
//
// The console form deliberately drops the timestamp, the caller and the
// component field. A user running one command in the foreground knows what time
// it is and does not care which Go file emitted the line; what they need is the
// severity, the sentence, and any error attached to it.
func SetupLogging(level, format string, output io.Writer) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	zerolog.TimeFieldFormat = time.RFC3339

	if format == LogFormatConsole || format == LogFormatText {
		log.Logger = zerolog.New(consoleWriter(output))
		return
	}

	log.Logger = zerolog.New(output).With().Timestamp().Caller().Logger()
}

// consoleWriter builds the human-readable writer.
func consoleWriter(output io.Writer) zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:     output,
		NoColor: !isTerminal(output),
		// Timestamp and caller are noise in a foreground command.
		PartsExclude: []string{
			zerolog.TimestampFieldName,
			zerolog.CallerFieldName,
		},
		// "component" tells a user which package logged; it does not tell them
		// anything they can act on.
		FieldsExclude: []string{"component"},
		// "warning:" rather than zerolog's "WRN", to match the sentence-shaped
		// diagnostics the rest of the CLI prints.
		FormatLevel: func(i interface{}) string {
			switch strings.ToLower(fmt.Sprint(i)) {
			case "trace", "debug":
				return "debug:"
			case "info":
				return "info:"
			case "warn":
				return "warning:"
			case "error":
				return "error:"
			case "fatal":
				return "fatal:"
			case "panic":
				return "panic:"
			}
			return fmt.Sprint(i) + ":"
		},
		// The error carries the remedy (see internal/embed installHint), so it
		// is printed as prose rather than as error="...".
		// An em dash rather than "error=": the error continues the sentence the
		// message started. zerolog writes the separating space itself.
		FormatErrFieldName:  func(interface{}) string { return "— " },
		FormatErrFieldValue: consoleProse,
		FormatFieldValue:    consoleProse,
	}
}

// consoleProse undoes zerolog's defensive quoting for terminal output.
//
// ConsoleWriter runs any field value containing a space through strconv.Quote
// before handing it to the formatter, which is right for a field whose bounds
// have to be unambiguous and wrong for a sentence. Without this, the
// llama-server guidance -- which contains both spaces and backticked commands
// with quotes in them -- reaches the user as
//
//	error="... configured binary_path \"/x/llama-server\" does not exist; ..."
//
// Unquoting it back to prose is the entire point of having a console format.
func consoleProse(i interface{}) string {
	s := fmt.Sprint(i)
	if unquoted, err := strconv.Unquote(s); err == nil {
		return unquoted
	}
	return s
}

// isTerminal reports whether colour escapes are appropriate for output.
//
// Written by hand rather than pulled in from x/term: the check is four lines
// and Conduit does not take a dependency for four lines. NO_COLOR and
// TERM=dumb are honoured because CI logs and installer transcripts are read as
// plain text.
func isTerminal(output io.Writer) bool {
	f, ok := output.(*os.File)
	if !ok {
		return false
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || os.Getenv("TERM") == "" {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// SetupDefaultLogging sets up structured JSON logging on stderr.
//
// This is the MACHINE-facing setup. `conduit mcp kb` uses it unconditionally:
// its stderr is read by an AI client's process supervisor, not by a person, and
// its stdout is the MCP frame stream and must stay protocol-pure. Interactive
// commands go through SetupConsoleLogging instead.
func SetupDefaultLogging(level string) {
	SetupLogging(level, LogFormatJSON, os.Stderr)
}

// SetupConsoleLogging sets up human-readable logging on stderr.
func SetupConsoleLogging(level string) {
	SetupLogging(level, LogFormatConsole, os.Stderr)
}

// Logger returns a contextualized logger for a component.
func Logger(component string) zerolog.Logger {
	return log.With().Str("component", component).Logger()
}

// WithInstanceID adds instance ID to logger context.
func WithInstanceID(logger zerolog.Logger, instanceID string) zerolog.Logger {
	return logger.With().Str("instance_id", instanceID).Logger()
}

// WithClientID adds client ID to logger context.
func WithClientID(logger zerolog.Logger, clientID string) zerolog.Logger {
	return logger.With().Str("client_id", clientID).Logger()
}

// WithRequestID adds request ID to logger context.
func WithRequestID(logger zerolog.Logger, requestID string) zerolog.Logger {
	return logger.With().Str("request_id", requestID).Logger()
}

// Event types for structured logging
const (
	EventInstanceCreated = "instance_created"
	EventInstanceStarted = "instance_started"
	EventInstanceStopped = "instance_stopped"
	EventInstanceFailed  = "instance_failed"
	EventInstanceRemoved = "instance_removed"
	EventBindingCreated  = "binding_created"
	EventBindingRemoved  = "binding_removed"
	EventKBSourceAdded   = "kb_source_added"
	EventKBSourceRemoved = "kb_source_removed"
	EventKBSyncCompleted = "kb_sync_completed"
	EventAuditCompleted  = "audit_completed"
	EventPolicyDecision  = "policy_decision"
	EventSecretAccessed  = "secret_accessed"
	EventDaemonStarted   = "daemon_started"
	EventDaemonStopped   = "daemon_stopped"
	EventHealthCheck     = "health_check"
)

// LogEvent logs a structured event.
func LogEvent(logger zerolog.Logger, event string, fields map[string]interface{}) {
	e := logger.Info().Str("event", event)
	for k, v := range fields {
		e = e.Interface(k, v)
	}
	e.Msg("")
}

// LogError logs an error with context.
func LogError(logger zerolog.Logger, err error, message string, fields map[string]interface{}) {
	e := logger.Error().Err(err)
	for k, v := range fields {
		e = e.Interface(k, v)
	}
	e.Msg(message)
}

// SanitizeForLog removes sensitive data from a map before logging.
func SanitizeForLog(data map[string]interface{}) map[string]interface{} {
	sanitized := make(map[string]interface{})
	sensitiveKeys := map[string]bool{
		"password":     true,
		"secret":       true,
		"token":        true,
		"api_key":      true,
		"apikey":       true,
		"access_token": true,
		"private_key":  true,
		"credentials":  true,
	}

	for k, v := range data {
		if sensitiveKeys[k] {
			sanitized[k] = "[REDACTED]"
		} else {
			sanitized[k] = v
		}
	}

	return sanitized
}

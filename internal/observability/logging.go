// Package observability provides logging, metrics, and tracing for Conduit.
package observability

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// SetupLogging configures the global logger based on the provided settings.
func SetupLogging(level, format string, output io.Writer) {
	// Parse log level
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	// Configure time format
	zerolog.TimeFieldFormat = time.RFC3339

	// Set output format
	if format == "console" || format == "text" {
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: "15:04:05",
		}
	}

	// Set global logger
	log.Logger = zerolog.New(output).With().Timestamp().Caller().Logger()
}

// SetupDefaultLogging sets up logging with sensible defaults.
func SetupDefaultLogging(level string) {
	SetupLogging(level, "json", os.Stderr)
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
	EventInstanceCreated  = "instance_created"
	EventInstanceStarted  = "instance_started"
	EventInstanceStopped  = "instance_stopped"
	EventInstanceFailed   = "instance_failed"
	EventInstanceRemoved  = "instance_removed"
	EventBindingCreated   = "binding_created"
	EventBindingRemoved   = "binding_removed"
	EventKBSourceAdded    = "kb_source_added"
	EventKBSourceRemoved  = "kb_source_removed"
	EventKBSyncCompleted  = "kb_sync_completed"
	EventAuditCompleted   = "audit_completed"
	EventPolicyDecision   = "policy_decision"
	EventSecretAccessed   = "secret_accessed"
	EventDaemonStarted    = "daemon_started"
	EventDaemonStopped    = "daemon_stopped"
	EventHealthCheck      = "health_check"
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

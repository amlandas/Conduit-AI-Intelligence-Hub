package models

import "fmt"

// ErrorCode represents a Conduit error code.
type ErrorCode string

// Error codes for Conduit operations.
const (
	// Runtime errors
	ErrRuntimeNotFound    ErrorCode = "E_RUNTIME_NOT_FOUND"
	ErrRuntimeUnavailable ErrorCode = "E_RUNTIME_UNAVAILABLE"
	ErrContainerFailed    ErrorCode = "E_CONTAINER_FAILED"
	ErrContainerNotFound  ErrorCode = "E_CONTAINER_NOT_FOUND"

	// Permission errors
	ErrPermissionDenied   ErrorCode = "E_PERMISSION_DENIED"
	ErrPermissionRequired ErrorCode = "E_PERMISSION_REQUIRED"

	// Lifecycle errors
	ErrInvalidTransition ErrorCode = "E_INVALID_TRANSITION"
	ErrInstanceNotFound  ErrorCode = "E_INSTANCE_NOT_FOUND"
	ErrInstanceExists    ErrorCode = "E_INSTANCE_EXISTS"

	// Audit errors
	ErrAuditFailed  ErrorCode = "E_AUDIT_FAILED"
	ErrAuditBlocked ErrorCode = "E_AUDIT_BLOCKED"

	// Configuration errors
	ErrConfigInvalid   ErrorCode = "E_CONFIG_INVALID"
	ErrConfigNotFound  ErrorCode = "E_CONFIG_NOT_FOUND"
	ErrConfigWriteFail ErrorCode = "E_CONFIG_WRITE_FAIL"

	// Client errors
	ErrClientNotFound    ErrorCode = "E_CLIENT_NOT_FOUND"
	ErrClientNotInstalled ErrorCode = "E_CLIENT_NOT_INSTALLED"
	ErrBindingNotFound   ErrorCode = "E_BINDING_NOT_FOUND"
	ErrBindingExists     ErrorCode = "E_BINDING_EXISTS"

	// KB errors
	ErrSourceNotFound  ErrorCode = "E_SOURCE_NOT_FOUND"
	ErrSourceExists    ErrorCode = "E_SOURCE_EXISTS"
	ErrPathNotFound    ErrorCode = "E_PATH_NOT_FOUND"
	ErrPathNotReadable ErrorCode = "E_PATH_NOT_READABLE"
	ErrIndexFailed     ErrorCode = "E_INDEX_FAILED"

	// Secret errors
	ErrSecretNotFound ErrorCode = "E_SECRET_NOT_FOUND"
	ErrSecretAccess   ErrorCode = "E_SECRET_ACCESS"

	// Package errors
	ErrPackageInvalid  ErrorCode = "E_PACKAGE_INVALID"
	ErrPackageNotFound ErrorCode = "E_PACKAGE_NOT_FOUND"
	ErrImagePullFailed ErrorCode = "E_IMAGE_PULL_FAILED"

	// Daemon errors
	ErrDaemonNotRunning  ErrorCode = "E_DAEMON_NOT_RUNNING"
	ErrDaemonUnavailable ErrorCode = "E_DAEMON_UNAVAILABLE"
)

// ConduitError represents a structured error with code and context.
type ConduitError struct {
	Code    ErrorCode              `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
	Cause   error                  `json:"-"`
}

// Error implements the error interface.
func (e *ConduitError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause for errors.Is/As.
func (e *ConduitError) Unwrap() error {
	return e.Cause
}

// NewError creates a new ConduitError.
func NewError(code ErrorCode, message string) *ConduitError {
	return &ConduitError{
		Code:    code,
		Message: message,
	}
}

// WithDetails adds details to the error.
func (e *ConduitError) WithDetails(key string, value interface{}) *ConduitError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithCause adds a cause to the error.
func (e *ConduitError) WithCause(cause error) *ConduitError {
	e.Cause = cause
	return e
}

// Wrap wraps an error with a ConduitError.
func Wrap(code ErrorCode, message string, cause error) *ConduitError {
	return &ConduitError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

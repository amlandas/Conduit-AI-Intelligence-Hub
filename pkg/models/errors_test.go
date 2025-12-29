package models

import (
	"errors"
	"strings"
	"testing"
)

func TestNewError(t *testing.T) {
	err := NewError(ErrRuntimeNotFound, "runtime not found")

	if err.Code != ErrRuntimeNotFound {
		t.Errorf("Code mismatch: got %s, want %s", err.Code, ErrRuntimeNotFound)
	}
	if err.Message != "runtime not found" {
		t.Errorf("Message mismatch: got %s", err.Message)
	}
	if err.Cause != nil {
		t.Error("Cause should be nil")
	}
	if err.Details != nil {
		t.Error("Details should be nil")
	}
}

func TestConduitError_Error(t *testing.T) {
	err := NewError(ErrRuntimeNotFound, "runtime not found")

	errStr := err.Error()
	if !strings.Contains(errStr, string(ErrRuntimeNotFound)) {
		t.Errorf("Error string should contain code: %s", errStr)
	}
	if !strings.Contains(errStr, "runtime not found") {
		t.Errorf("Error string should contain message: %s", errStr)
	}
}

func TestConduitError_ErrorWithCause(t *testing.T) {
	cause := errors.New("underlying error")
	err := NewError(ErrRuntimeNotFound, "runtime not found").WithCause(cause)

	errStr := err.Error()
	if !strings.Contains(errStr, "underlying error") {
		t.Errorf("Error string should contain cause: %s", errStr)
	}
}

func TestConduitError_WithDetails(t *testing.T) {
	err := NewError(ErrInstanceNotFound, "instance not found").
		WithDetails("instance_id", "inst_123").
		WithDetails("package_id", "test/pkg")

	if err.Details == nil {
		t.Fatal("Details should not be nil")
	}
	if err.Details["instance_id"] != "inst_123" {
		t.Error("Details should contain instance_id")
	}
	if err.Details["package_id"] != "test/pkg" {
		t.Error("Details should contain package_id")
	}
}

func TestConduitError_WithCause(t *testing.T) {
	cause := errors.New("underlying error")
	err := NewError(ErrContainerFailed, "container failed").WithCause(cause)

	if err.Cause != cause {
		t.Error("Cause should be set")
	}
}

func TestConduitError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := NewError(ErrContainerFailed, "container failed").WithCause(cause)

	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Error("Unwrap should return cause")
	}
}

func TestConduitError_Unwrap_NoCause(t *testing.T) {
	err := NewError(ErrContainerFailed, "container failed")

	unwrapped := err.Unwrap()
	if unwrapped != nil {
		t.Error("Unwrap should return nil when no cause")
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := Wrap(ErrContainerFailed, "container failed", cause)

	if err.Code != ErrContainerFailed {
		t.Errorf("Code mismatch: got %s", err.Code)
	}
	if err.Message != "container failed" {
		t.Errorf("Message mismatch: got %s", err.Message)
	}
	if err.Cause != cause {
		t.Error("Cause should be set")
	}
}

func TestErrorCodes(t *testing.T) {
	// Verify all error codes are unique
	codes := map[ErrorCode]bool{
		ErrRuntimeNotFound:     true,
		ErrRuntimeUnavailable:  true,
		ErrContainerFailed:     true,
		ErrContainerNotFound:   true,
		ErrPermissionDenied:    true,
		ErrPermissionRequired:  true,
		ErrInvalidTransition:   true,
		ErrInstanceNotFound:    true,
		ErrInstanceExists:      true,
		ErrAuditFailed:         true,
		ErrAuditBlocked:        true,
		ErrConfigInvalid:       true,
		ErrConfigNotFound:      true,
		ErrConfigWriteFail:     true,
		ErrClientNotFound:      true,
		ErrClientNotInstalled:  true,
		ErrBindingNotFound:     true,
		ErrBindingExists:       true,
		ErrSourceNotFound:      true,
		ErrSourceExists:        true,
		ErrPathNotFound:        true,
		ErrPathNotReadable:     true,
		ErrIndexFailed:         true,
		ErrSecretNotFound:      true,
		ErrSecretAccess:        true,
		ErrPackageInvalid:      true,
		ErrPackageNotFound:     true,
		ErrImagePullFailed:     true,
		ErrDaemonNotRunning:    true,
		ErrDaemonUnavailable:   true,
	}

	// All codes should be unique (map would just overwrite if not)
	if len(codes) != 30 {
		t.Errorf("Expected 30 unique error codes, got %d", len(codes))
	}
}

func TestConduitError_ChainMethods(t *testing.T) {
	cause := errors.New("root cause")

	err := NewError(ErrInstanceNotFound, "not found").
		WithDetails("key", "value").
		WithCause(cause)

	if err.Details["key"] != "value" {
		t.Error("Chain: Details should be set")
	}
	if err.Cause != cause {
		t.Error("Chain: Cause should be set")
	}
}

func TestErrorsIs(t *testing.T) {
	cause := errors.New("specific cause")
	err := Wrap(ErrContainerFailed, "wrapper", cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is should find cause")
	}
}

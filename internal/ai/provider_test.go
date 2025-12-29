package ai

import (
	"testing"
)

func TestDefaultProviderConfig(t *testing.T) {
	cfg := DefaultProviderConfig()

	if cfg.Provider != "ollama" {
		t.Errorf("expected provider ollama, got %s", cfg.Provider)
	}

	if cfg.Model != "qwen2.5-coder:7b" {
		t.Errorf("expected model qwen2.5-coder:7b, got %s", cfg.Model)
	}

	if cfg.Endpoint != "http://localhost:11434" {
		t.Errorf("expected endpoint http://localhost:11434, got %s", cfg.Endpoint)
	}

	if cfg.TimeoutSeconds != 120 {
		t.Errorf("expected timeout 120, got %d", cfg.TimeoutSeconds)
	}

	if cfg.MaxRetries != 2 {
		t.Errorf("expected max retries 2, got %d", cfg.MaxRetries)
	}

	if cfg.ConfidenceThreshold != 0.6 {
		t.Errorf("expected confidence threshold 0.6, got %f", cfg.ConfidenceThreshold)
	}
}

func TestErrLowConfidence(t *testing.T) {
	err := &ErrLowConfidence{
		Confidence: 0.45,
		Threshold:  0.6,
		Message:    "Analysis uncertain",
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}

	// Should contain percentages
	if !containsStr(errStr, "45%") {
		t.Errorf("expected error to contain confidence percentage, got: %s", errStr)
	}
	if !containsStr(errStr, "60%") {
		t.Errorf("expected error to contain threshold percentage, got: %s", errStr)
	}
}

func TestErrProviderUnavailable(t *testing.T) {
	err := &ErrProviderUnavailable{
		Provider: "ollama",
		Reason:   "connection refused",
	}

	errStr := err.Error()
	if !containsStr(errStr, "ollama") {
		t.Errorf("expected error to contain provider name, got: %s", errStr)
	}
	if !containsStr(errStr, "connection refused") {
		t.Errorf("expected error to contain reason, got: %s", errStr)
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

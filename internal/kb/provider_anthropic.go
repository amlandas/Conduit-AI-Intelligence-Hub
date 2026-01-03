// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// provider_anthropic.go implements the Anthropic LLM provider for cloud entity extraction.
package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// AnthropicProvider implements LLMProvider using Anthropic API.
// Claude 3.5 Haiku is recommended for fast, cost-effective extraction.
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// AnthropicProviderConfig holds Anthropic provider configuration.
type AnthropicProviderConfig struct {
	APIKey  string
	Model   string
	Timeout time.Duration
}

// NewAnthropicProvider creates a new Anthropic provider.
// Security: API key should be set via environment variable ANTHROPIC_API_KEY.
func NewAnthropicProvider(cfg AnthropicProviderConfig) (*AnthropicProvider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("Anthropic API key not configured: set ANTHROPIC_API_KEY environment variable")
	}

	if cfg.Model == "" {
		cfg.Model = "claude-3-5-haiku-latest"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}

	return &AnthropicProvider{
		apiKey: apiKey,
		model:  cfg.Model,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

// Name returns the provider name.
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// IsAvailable checks if Anthropic API is available.
func (p *AnthropicProvider) IsAvailable(ctx context.Context) bool {
	// Anthropic doesn't have a simple health endpoint, just check if we have a key
	return p.apiKey != ""
}

// ExtractEntities extracts entities and relations using Anthropic.
func (p *AnthropicProvider) ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()

	// Generate prompt
	prompt := ExtractionPrompt(req)

	// Build Anthropic request
	anthropicReq := anthropicMessagesRequest{
		Model: p.model,
		Messages: []anthropicMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		System:    "You are an expert knowledge graph extractor. Extract entities and relationships from text and return valid JSON only.",
		MaxTokens: 2048,
	}

	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Send request to Anthropic
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMProviderNotAvailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse Anthropic response
	var anthropicResp anthropicMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("no response from Anthropic")
	}

	// Find text content
	var textContent string
	for _, c := range anthropicResp.Content {
		if c.Type == "text" {
			textContent = c.Text
			break
		}
	}

	if textContent == "" {
		return nil, fmt.Errorf("no text content in Anthropic response")
	}

	// Parse extracted entities from LLM response
	extracted, err := parseExtractionResponse(textContent)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidExtractionResponse, err)
	}

	// Apply confidence threshold filtering
	filtered := filterByConfidence(extracted, req.ConfidenceThreshold)

	// Apply limits
	if len(filtered.Entities) > req.MaxEntities {
		filtered.Entities = filtered.Entities[:req.MaxEntities]
	}
	if len(filtered.Relations) > req.MaxRelations {
		filtered.Relations = filtered.Relations[:req.MaxRelations]
	}

	filtered.ProcessingTimeMs = time.Since(startTime).Milliseconds()
	filtered.TokensUsed = anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens
	filtered.Model = p.model

	return filtered, nil
}

// Close releases resources.
func (p *AnthropicProvider) Close() error {
	return nil
}

// Anthropic API types

type anthropicMessagesRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessagesResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

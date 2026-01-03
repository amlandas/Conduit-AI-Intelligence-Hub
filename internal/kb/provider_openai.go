// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// provider_openai.go implements the OpenAI LLM provider for cloud entity extraction.
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

// OpenAIProvider implements LLMProvider using OpenAI API.
// GPT-4o-mini is recommended for cost-effective extraction.
type OpenAIProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// OpenAIProviderConfig holds OpenAI provider configuration.
type OpenAIProviderConfig struct {
	APIKey  string
	Model   string
	BaseURL string
	Timeout time.Duration
}

// NewOpenAIProvider creates a new OpenAI provider.
// Security: API key should be set via environment variable OPENAI_API_KEY.
func NewOpenAIProvider(cfg OpenAIProviderConfig) (*OpenAIProvider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key not configured: set OPENAI_API_KEY environment variable")
	}

	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}

	return &OpenAIProvider{
		apiKey:  apiKey,
		model:   cfg.Model,
		baseURL: cfg.BaseURL,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// IsAvailable checks if OpenAI API is available.
func (p *OpenAIProvider) IsAvailable(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// ExtractEntities extracts entities and relations using OpenAI.
func (p *OpenAIProvider) ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()

	// Generate prompt
	prompt := ExtractionPrompt(req)

	// Build OpenAI request
	openaiReq := openAIChatRequest{
		Model: p.model,
		Messages: []openAIMessage{
			{
				Role:    "system",
				Content: "You are an expert knowledge graph extractor. Extract entities and relationships from text and return valid JSON.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.1,
		MaxTokens:   2048,
	}

	reqBody, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Send request to OpenAI
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMProviderNotAvailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse OpenAI response
	var openaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	// Parse extracted entities from LLM response
	extracted, err := parseExtractionResponse(openaiResp.Choices[0].Message.Content)
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
	filtered.TokensUsed = openaiResp.Usage.TotalTokens
	filtered.Model = p.model

	return filtered, nil
}

// Close releases resources.
func (p *OpenAIProvider) Close() error {
	return nil
}

// OpenAI API types

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

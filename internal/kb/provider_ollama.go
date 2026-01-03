// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// provider_ollama.go implements the Ollama LLM provider for local entity extraction.
package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaProvider implements LLMProvider using local Ollama.
// Mistral 7B is recommended for entity extraction (Apache 2.0, best F1 for NER).
type OllamaProvider struct {
	host      string
	model     string
	keepAlive string
	client    *http.Client
}

// OllamaProviderConfig holds Ollama provider configuration.
type OllamaProviderConfig struct {
	Host      string
	Model     string
	KeepAlive string
	Timeout   time.Duration
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(cfg OllamaProviderConfig) (*OllamaProvider, error) {
	if cfg.Host == "" {
		cfg.Host = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "mistral:7b-instruct-q4_K_M"
	}
	if cfg.KeepAlive == "" {
		cfg.KeepAlive = "5m"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 300 * time.Second // 5 minutes - allows for model loading on first run
	}

	return &OllamaProvider{
		host:      cfg.Host,
		model:     cfg.Model,
		keepAlive: cfg.KeepAlive,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

// Name returns the provider name.
func (p *OllamaProvider) Name() string {
	return "ollama"
}

// IsAvailable checks if Ollama is available.
func (p *OllamaProvider) IsAvailable(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", p.host+"/api/version", nil)
	if err != nil {
		return false
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// ExtractEntities extracts entities and relations using Ollama.
func (p *OllamaProvider) ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()

	// Generate prompt
	prompt := ExtractionPrompt(req)

	// Build Ollama request
	ollamaReq := ollamaGenerateRequest{
		Model:     p.model,
		Prompt:    prompt,
		Stream:    false,
		KeepAlive: p.keepAlive,
		Options: ollamaOptions{
			Temperature: 0.1, // Low temperature for consistent extraction
			NumPredict:  2048,
		},
	}

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Send request to Ollama
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.host+"/api/generate", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMProviderNotAvailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse Ollama response
	var ollamaResp ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Parse extracted entities from LLM response
	extracted, err := parseExtractionResponse(ollamaResp.Response)
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
	filtered.TokensUsed = int(ollamaResp.TotalDuration / 1000000) // Approximate tokens from duration
	filtered.Model = p.model

	return filtered, nil
}

// Close releases resources.
func (p *OllamaProvider) Close() error {
	return nil
}

// Ollama API types

type ollamaGenerateRequest struct {
	Model     string        `json:"model"`
	Prompt    string        `json:"prompt"`
	Stream    bool          `json:"stream"`
	KeepAlive string        `json:"keep_alive,omitempty"`
	Options   ollamaOptions `json:"options,omitempty"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaGenerateResponse struct {
	Model              string `json:"model"`
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	TotalDuration      int64  `json:"total_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	EvalCount          int    `json:"eval_count"`
}

// parseExtractionResponse parses the LLM JSON response into entities and relations.
func parseExtractionResponse(response string) (*ExtractionResponse, error) {
	// Find JSON in response (LLM might include preamble)
	jsonStart := findJSONStart(response)
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in response")
	}
	jsonStr := response[jsonStart:]

	// Find matching closing brace
	jsonEnd := findJSONEnd(jsonStr)
	if jsonEnd == -1 {
		return nil, fmt.Errorf("incomplete JSON in response")
	}
	jsonStr = jsonStr[:jsonEnd+1]

	// Parse JSON
	var raw struct {
		Entities  []ExtractedEntity  `json:"entities"`
		Relations []ExtractedRelation `json:"relations"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	// Validate and normalize entities
	validEntities := make([]ExtractedEntity, 0, len(raw.Entities))
	for _, e := range raw.Entities {
		// Normalize entity type
		e.Type = normalizeEntityType(e.Type)
		if err := e.Validate(); err == nil {
			validEntities = append(validEntities, e)
		}
	}

	// Validate and normalize relations
	validRelations := make([]ExtractedRelation, 0, len(raw.Relations))
	for _, r := range raw.Relations {
		// Normalize predicate
		r.Predicate = normalizeRelationType(r.Predicate)
		if err := r.Validate(); err == nil {
			validRelations = append(validRelations, r)
		}
	}

	return &ExtractionResponse{
		Entities:  validEntities,
		Relations: validRelations,
	}, nil
}

// findJSONStart finds the start of JSON object in response.
func findJSONStart(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			return i
		}
	}
	return -1
}

// findJSONEnd finds the matching closing brace.
func findJSONEnd(s string) int {
	depth := 0
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// filterByConfidence filters entities and relations by confidence threshold.
func filterByConfidence(resp *ExtractionResponse, threshold float64) *ExtractionResponse {
	if threshold <= 0 {
		return resp
	}

	filteredEntities := make([]ExtractedEntity, 0)
	for _, e := range resp.Entities {
		if e.Confidence >= threshold {
			filteredEntities = append(filteredEntities, e)
		}
	}

	filteredRelations := make([]ExtractedRelation, 0)
	for _, r := range resp.Relations {
		if r.Confidence >= threshold {
			filteredRelations = append(filteredRelations, r)
		}
	}

	return &ExtractionResponse{
		Entities:  filteredEntities,
		Relations: filteredRelations,
	}
}

// normalizeEntityType maps various entity type strings to valid EntityType values.
func normalizeEntityType(t string) string {
	lower := stringToLower(t)

	switch lower {
	case "concept", "idea", "topic", "theme":
		return string(EntityTypeConcept)
	case "organization", "company", "institution", "org", "group":
		return string(EntityTypeOrganization)
	case "person", "individual", "human", "people":
		return string(EntityTypePerson)
	case "section", "heading", "chapter":
		return string(EntityTypeSection)
	case "document", "doc", "file":
		return string(EntityTypeDocument)
	case "technology", "tech", "tool", "framework", "protocol", "software":
		return string(EntityTypeTechnology)
	case "location", "place", "region", "country", "city":
		return string(EntityTypeLocation)
	case "event", "incident", "occurrence":
		return string(EntityTypeEvent)
	default:
		return string(EntityTypeConcept) // Default
	}
}

// normalizeRelationType maps various relation type strings to valid RelationType values.
func normalizeRelationType(r string) string {
	lower := stringToLower(r)

	switch lower {
	case "mentions", "reference", "refers_to", "cites":
		return string(RelationMentions)
	case "defines", "definition", "explains":
		return string(RelationDefines)
	case "relates_to", "related", "associated", "connected":
		return string(RelationRelatesTo)
	case "contains", "includes", "has":
		return string(RelationContains)
	case "part_of", "belongs_to", "member_of":
		return string(RelationPartOf)
	case "implements", "uses", "applies":
		return string(RelationImplements)
	case "depends_on", "requires", "needs":
		return string(RelationDependsOn)
	case "created_by", "authored_by", "developed_by":
		return string(RelationCreatedBy)
	case "used_by", "utilized_by", "employed_by":
		return string(RelationUsedBy)
	case "similar_to", "like", "resembles":
		return string(RelationSimilarTo)
	default:
		return string(RelationRelatesTo) // Default
	}
}

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
	"strings"
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
		cfg.KeepAlive = "30m" // Keep model loaded for 30 minutes between requests
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

// WarmUp preloads the model into memory by making a minimal extraction request.
// This eliminates cold-start delays on first actual use.
// The model will stay loaded based on the KeepAlive setting (default: 5 minutes).
func (p *OllamaProvider) WarmUp(ctx context.Context) error {
	// Make a minimal extraction request to trigger model loading
	req := &ExtractionRequest{
		Content:             "System warmup test.",
		MaxEntities:         1,
		MaxRelations:        1,
		ConfidenceThreshold: 0.9, // High threshold to minimize processing
	}
	_, err := p.ExtractEntities(ctx, req)
	return err
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

// sanitizeJSONString cleans up common LLM JSON quirks before parsing.
// Handles invalid escape sequences like \_ (LaTeX-style) that break JSON parsers.
func sanitizeJSONString(s string) string {
	// Replace common invalid escape sequences
	// \_ is common in LaTeX-style text (e.g., "machine\_learning")
	result := strings.ReplaceAll(s, "\\_", "_")
	// \* for emphasis markers
	result = strings.ReplaceAll(result, "\\*", "*")
	// \# for headers
	result = strings.ReplaceAll(result, "\\#", "#")
	// \[ and \] for math notation
	result = strings.ReplaceAll(result, "\\[", "[")
	result = strings.ReplaceAll(result, "\\]", "]")
	return result
}

// salvageIncompleteJSON attempts to recover valid data from truncated JSON.
// It tries to extract entities even if the relations section is incomplete.
func salvageIncompleteJSON(jsonStr string) ([]ExtractedEntity, error) {
	// Try to find and parse just the entities array
	entitiesStart := strings.Index(jsonStr, `"entities"`)
	if entitiesStart == -1 {
		return nil, fmt.Errorf("no entities field found")
	}

	// Find the start of the entities array
	arrayStart := strings.Index(jsonStr[entitiesStart:], "[")
	if arrayStart == -1 {
		return nil, fmt.Errorf("no entities array found")
	}
	arrayStart += entitiesStart

	// Try to find the end of entities array
	depth := 0
	arrayEnd := -1
	inString := false
	escaped := false

	for i := arrayStart; i < len(jsonStr); i++ {
		c := jsonStr[i]
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
		if c == '[' {
			depth++
		} else if c == ']' {
			depth--
			if depth == 0 {
				arrayEnd = i
				break
			}
		}
	}

	if arrayEnd == -1 {
		// Array is incomplete - try to salvage by closing it
		// Find the last complete object
		lastObjEnd := strings.LastIndex(jsonStr, "}")
		if lastObjEnd > arrayStart {
			jsonStr = jsonStr[:lastObjEnd+1] + "]"
			arrayEnd = len(jsonStr) - 1
		} else {
			return nil, fmt.Errorf("cannot salvage entities array")
		}
	}

	entitiesJSON := jsonStr[arrayStart : arrayEnd+1]

	// Parse as flexible entities
	var entities []flexibleEntity
	if err := json.Unmarshal([]byte(entitiesJSON), &entities); err != nil {
		return nil, fmt.Errorf("salvage parse failed: %w", err)
	}

	// Convert to ExtractedEntity
	result := make([]ExtractedEntity, 0, len(entities))
	for _, fe := range entities {
		e := fe.toExtractedEntity()
		if e.Name != "" {
			result = append(result, e)
		}
	}

	return result, nil
}

// flexibleEntity handles JSON with type mismatches (e.g., arrays instead of strings).
type flexibleEntity struct {
	Name        interface{} `json:"name"`
	Type        interface{} `json:"type"`
	Description interface{} `json:"description"`
	Confidence  interface{} `json:"confidence"`
}

// toExtractedEntity converts flexible entity to ExtractedEntity, coercing types.
func (fe *flexibleEntity) toExtractedEntity() ExtractedEntity {
	return ExtractedEntity{
		Name:        coerceToString(fe.Name),
		Type:        coerceToString(fe.Type),
		Description: coerceToString(fe.Description),
		Confidence:  coerceToFloat(fe.Confidence),
	}
}

// flexibleRelation handles JSON with type mismatches for relations.
type flexibleRelation struct {
	Subject    interface{} `json:"subject"`
	Predicate  interface{} `json:"predicate"`
	Object     interface{} `json:"object"`
	Confidence interface{} `json:"confidence"`
}

// toExtractedRelation converts flexible relation to ExtractedRelation.
func (fr *flexibleRelation) toExtractedRelation() ExtractedRelation {
	return ExtractedRelation{
		Subject:    coerceToString(fr.Subject),
		Predicate:  coerceToString(fr.Predicate),
		Object:     coerceToString(fr.Object),
		Confidence: coerceToFloat(fr.Confidence),
	}
}

// coerceToString converts various types to string, joining arrays with ", ".
func coerceToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		// Join array elements with comma
		parts := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case float64:
		return fmt.Sprintf("%.0f", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// coerceToFloat converts various types to float64.
func coerceToFloat(v interface{}) float64 {
	if v == nil {
		return 0.8 // Default confidence
	}
	switch val := v.(type) {
	case float64:
		return val
	case string:
		// Try to parse string as float
		var f float64
		if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
			return f
		}
		return 0.8
	case int:
		return float64(val)
	default:
		return 0.8
	}
}

// parseExtractionResponse parses the LLM JSON response into entities and relations.
// Uses a multi-stage approach: sanitize → try parse → on fail: salvage → flexible unmarshal.
func parseExtractionResponse(response string) (*ExtractionResponse, error) {
	// Stage 1: Sanitize common LLM JSON quirks
	response = sanitizeJSONString(response)

	// Find JSON in response (LLM might include preamble)
	jsonStart := findJSONStart(response)
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in response")
	}
	jsonStr := response[jsonStart:]

	// Find matching closing brace
	jsonEnd := findJSONEnd(jsonStr)
	if jsonEnd == -1 {
		// Stage 2: Try to salvage incomplete JSON
		entities, err := salvageIncompleteJSON(jsonStr)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrIncompleteJSON, err)
		}
		// Successfully salvaged entities
		validEntities := make([]ExtractedEntity, 0, len(entities))
		for _, e := range entities {
			e.Type = normalizeEntityType(e.Type)
			if err := e.Validate(); err == nil {
				validEntities = append(validEntities, e)
			}
		}
		return &ExtractionResponse{
			Entities:  validEntities,
			Relations: []ExtractedRelation{}, // Relations lost in truncation
		}, nil
	}
	jsonStr = jsonStr[:jsonEnd+1]

	// Stage 3: Try flexible unmarshaling first (handles type mismatches)
	var rawFlex struct {
		Entities  []flexibleEntity   `json:"entities"`
		Relations []flexibleRelation `json:"relations"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawFlex); err != nil {
		// Stage 4: If flexible unmarshal fails, try salvage
		entities, salvageErr := salvageIncompleteJSON(jsonStr)
		if salvageErr != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		validEntities := make([]ExtractedEntity, 0, len(entities))
		for _, e := range entities {
			e.Type = normalizeEntityType(e.Type)
			if err := e.Validate(); err == nil {
				validEntities = append(validEntities, e)
			}
		}
		return &ExtractionResponse{
			Entities:  validEntities,
			Relations: []ExtractedRelation{},
		}, nil
	}

	// Convert flexible entities to standard entities
	validEntities := make([]ExtractedEntity, 0, len(rawFlex.Entities))
	for _, fe := range rawFlex.Entities {
		e := fe.toExtractedEntity()
		// Normalize entity type
		e.Type = normalizeEntityType(e.Type)
		if err := e.Validate(); err == nil {
			validEntities = append(validEntities, e)
		}
	}

	// Convert flexible relations to standard relations
	validRelations := make([]ExtractedRelation, 0, len(rawFlex.Relations))
	for _, fr := range rawFlex.Relations {
		r := fr.toExtractedRelation()
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

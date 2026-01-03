// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// llm_provider.go defines the LLM provider interface for entity extraction.
package kb

import (
	"context"
	"fmt"
)

// LLMProvider defines the interface for LLM-based entity extraction.
// Implementations include Ollama (local), OpenAI, and Anthropic.
type LLMProvider interface {
	// Name returns the provider name (e.g., "ollama", "openai", "anthropic")
	Name() string

	// IsAvailable checks if the provider is available and ready
	IsAvailable(ctx context.Context) bool

	// ExtractEntities extracts entities and relations from text
	ExtractEntities(ctx context.Context, req *ExtractionRequest) (*ExtractionResponse, error)

	// Close releases any resources held by the provider
	Close() error
}

// ExtractionRequest represents a request to extract entities from text.
type ExtractionRequest struct {
	// ChunkID is the identifier of the chunk being processed
	ChunkID string `json:"chunk_id"`

	// DocumentID is the identifier of the source document
	DocumentID string `json:"document_id"`

	// DocumentTitle is the title of the source document
	DocumentTitle string `json:"document_title,omitempty"`

	// Content is the text to extract entities from
	Content string `json:"content"`

	// SectionHeading is the heading of the current section (if available)
	SectionHeading string `json:"section_heading,omitempty"`

	// MaxEntities limits the number of entities to extract
	MaxEntities int `json:"max_entities,omitempty"`

	// MaxRelations limits the number of relations to extract
	MaxRelations int `json:"max_relations,omitempty"`

	// ConfidenceThreshold filters low-confidence extractions
	ConfidenceThreshold float64 `json:"confidence_threshold,omitempty"`
}

// ExtractionResponse represents the result of entity extraction.
type ExtractionResponse struct {
	// Entities are the extracted entities
	Entities []ExtractedEntity `json:"entities"`

	// Relations are the extracted relations between entities
	Relations []ExtractedRelation `json:"relations"`

	// ProcessingTimeMs is the time taken to process in milliseconds
	ProcessingTimeMs int64 `json:"processing_time_ms"`

	// TokensUsed is the number of tokens used (if available)
	TokensUsed int `json:"tokens_used,omitempty"`

	// Model is the model used for extraction
	Model string `json:"model,omitempty"`
}

// ExtractedEntity represents an entity extracted by the LLM.
type ExtractedEntity struct {
	// Name is the entity name as extracted
	Name string `json:"name"`

	// Type is the entity type (concept, person, organization, etc.)
	Type string `json:"type"`

	// Description provides additional context
	Description string `json:"description,omitempty"`

	// Confidence is the extraction confidence (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// Aliases are alternative names for this entity
	Aliases []string `json:"aliases,omitempty"`
}

// ExtractedRelation represents a relation extracted by the LLM.
type ExtractedRelation struct {
	// Subject is the source entity name
	Subject string `json:"subject"`

	// Predicate is the relationship type
	Predicate string `json:"predicate"`

	// Object is the target entity name
	Object string `json:"object"`

	// Confidence is the extraction confidence (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// Description provides additional context about the relation
	Description string `json:"description,omitempty"`
}

// Validate checks the extraction request for errors.
func (r *ExtractionRequest) Validate() error {
	if r.Content == "" {
		return fmt.Errorf("extraction request: content cannot be empty")
	}
	if len(r.Content) > MaxQueryLength {
		return ErrQueryTooLong
	}
	if r.MaxEntities <= 0 {
		r.MaxEntities = MaxEntitiesPerChunk
	}
	if r.MaxRelations <= 0 {
		r.MaxRelations = MaxRelationsPerChunk
	}
	if r.ConfidenceThreshold <= 0 {
		r.ConfidenceThreshold = 0.7
	}
	return nil
}

// Validate checks the extracted entity for errors.
func (e *ExtractedEntity) Validate() error {
	if e.Name == "" {
		return ErrEmptyEntityName
	}
	if len(e.Name) > MaxEntityNameLength {
		return ErrEntityNameTooLong
	}
	if e.Type == "" {
		e.Type = string(EntityTypeConcept) // Default to concept
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return ErrInvalidConfidence
	}
	return nil
}

// Validate checks the extracted relation for errors.
func (r *ExtractedRelation) Validate() error {
	if r.Subject == "" || r.Object == "" {
		return fmt.Errorf("relation subject and object cannot be empty")
	}
	if r.Predicate == "" {
		r.Predicate = string(RelationRelatesTo) // Default
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return ErrInvalidConfidence
	}
	return nil
}

// ToEntity converts an extracted entity to a graph entity.
func (e *ExtractedEntity) ToEntity(chunkID, documentID string) *Entity {
	return &Entity{
		Name:             e.Name,
		Type:             EntityType(e.Type),
		Description:      e.Description,
		Confidence:       e.Confidence,
		SourceChunkID:    chunkID,
		SourceDocumentID: documentID,
	}
}

// ToRelation converts an extracted relation to a graph relation.
func (r *ExtractedRelation) ToRelation(chunkID string) *Relation {
	return &Relation{
		SubjectName: r.Subject,
		Predicate:   RelationType(r.Predicate),
		ObjectName:  r.Object,
		Confidence:  r.Confidence,
		SourceChunkID: chunkID,
	}
}

// ProviderFactory creates LLM providers based on configuration.
type ProviderFactory struct{}

// NewProviderFactory creates a new provider factory.
func NewProviderFactory() *ProviderFactory {
	return &ProviderFactory{}
}

// CreateProvider creates an LLM provider based on the given configuration.
func (f *ProviderFactory) CreateProvider(cfg KAGConfig) (LLMProvider, error) {
	switch cfg.Provider {
	case "ollama":
		return NewOllamaProvider(OllamaProviderConfig{
			Host:      cfg.Ollama.Host,
			Model:     cfg.Ollama.Model,
			KeepAlive: cfg.Ollama.KeepAlive,
		})
	case "openai":
		return NewOpenAIProvider(OpenAIProviderConfig{
			APIKey:  cfg.OpenAI.APIKey,
			Model:   cfg.OpenAI.Model,
			BaseURL: cfg.OpenAI.BaseURL,
		})
	case "anthropic":
		return NewAnthropicProvider(AnthropicProviderConfig{
			APIKey: cfg.Anthropic.APIKey,
			Model:  cfg.Anthropic.Model,
		})
	default:
		return nil, ErrInvalidLLMProvider
	}
}

// ExtractionPrompt generates the extraction prompt for a given request.
// Security: Uses structured prompts with clear delimiters to prevent injection.
func ExtractionPrompt(req *ExtractionRequest) string {
	return fmt.Sprintf(`You are an expert knowledge graph extractor. Extract entities and relationships from the following text.

<document_context>
Document: %s
Section: %s
</document_context>

<text_to_analyze>
%s
</text_to_analyze>

<extraction_rules>
1. Only extract entities that are EXPLICITLY mentioned in the text
2. Entity types: concept, organization, person, technology, location, event, section
3. Relation types: mentions, defines, relates_to, contains, part_of, implements, depends_on, created_by, used_by, similar_to
4. Assign confidence scores (0.0-1.0) based on how clearly the entity/relation is stated
5. Maximum %d entities, %d relations
6. Minimum confidence threshold: %.2f
</extraction_rules>

<output_format>
Respond ONLY with valid JSON in this exact format:
{
  "entities": [
    {"name": "entity name", "type": "concept|person|organization|technology|location|event|section", "description": "brief description", "confidence": 0.0-1.0}
  ],
  "relations": [
    {"subject": "entity1 name", "predicate": "mentions|defines|relates_to|contains|part_of|implements|depends_on|created_by|used_by|similar_to", "object": "entity2 name", "confidence": 0.0-1.0}
  ]
}
</output_format>

Extract entities and relations now:`,
		sanitizePromptInput(req.DocumentTitle),
		sanitizePromptInput(req.SectionHeading),
		sanitizePromptInput(req.Content),
		req.MaxEntities,
		req.MaxRelations,
		req.ConfidenceThreshold,
	)
}

// sanitizePromptInput removes potentially harmful content from prompt inputs.
// Security: Prevents prompt injection attacks.
func sanitizePromptInput(input string) string {
	if len(input) > 5000 {
		input = input[:5000] + "..."
	}
	// Remove potential prompt injection patterns
	// These are common attempts to escape the extraction context
	dangerous := []string{
		"</text_to_analyze>",
		"</document_context>",
		"</extraction_rules>",
		"</output_format>",
		"<text_to_analyze>",
		"<document_context>",
		"<extraction_rules>",
		"<output_format>",
		"Ignore previous instructions",
		"ignore all previous",
		"disregard the above",
		"forget everything",
		"system:",
		"assistant:",
		"user:",
	}

	for _, d := range dangerous {
		// Case-insensitive replacement
		input = replaceAllInsensitive(input, d, "[FILTERED]")
	}

	return input
}

// replaceAllInsensitive replaces all occurrences case-insensitively.
func replaceAllInsensitive(s, old, new string) string {
	lower := stringToLower(s)
	oldLower := stringToLower(old)

	result := s
	idx := 0
	for {
		pos := indexOf(lower[idx:], oldLower)
		if pos == -1 {
			break
		}
		actualPos := idx + pos
		result = result[:actualPos] + new + result[actualPos+len(old):]
		lower = lower[:actualPos] + new + lower[actualPos+len(old):]
		idx = actualPos + len(new)
	}
	return result
}

func stringToLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		} else {
			b[i] = c
		}
	}
	return string(b)
}

func indexOf(s, substr string) int {
	if len(substr) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

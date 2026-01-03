// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// extraction_validator.go validates and normalizes extracted entities and relations.
package kb

import (
	"regexp"
	"strings"
	"unicode"
)

// ExtractionValidator validates and normalizes extracted entities and relations.
// Security: Implements validation to filter low-quality and potentially injected content.
type ExtractionValidator struct {
	// minConfidence is the minimum confidence threshold for acceptance
	minConfidence float64
	// maxNameLength is the maximum allowed entity name length
	maxNameLength int
	// maxDescriptionLength is the maximum allowed description length
	maxDescriptionLength int
	// allowedEntityTypes is the set of valid entity types
	allowedEntityTypes map[EntityType]bool
	// allowedRelationTypes is the set of valid relation types
	allowedRelationTypes map[RelationType]bool
	// suspiciousPatterns are regex patterns that indicate potential injection
	suspiciousPatterns []*regexp.Regexp
}

// NewExtractionValidator creates a new validator with default settings.
func NewExtractionValidator() *ExtractionValidator {
	v := &ExtractionValidator{
		minConfidence:        0.5, // Lower than config threshold for validation
		maxNameLength:        MaxEntityNameLength,
		maxDescriptionLength: 2000,
		allowedEntityTypes: map[EntityType]bool{
			EntityTypeConcept:      true,
			EntityTypeOrganization: true,
			EntityTypePerson:       true,
			EntityTypeSection:      true,
			EntityTypeDocument:     true,
			EntityTypeTechnology:   true,
			EntityTypeLocation:     true,
			EntityTypeEvent:        true,
		},
		allowedRelationTypes: map[RelationType]bool{
			RelationMentions:   true,
			RelationDefines:    true,
			RelationRelatesTo:  true,
			RelationContains:   true,
			RelationPartOf:     true,
			RelationImplements: true,
			RelationDependsOn:  true,
			RelationCreatedBy:  true,
			RelationUsedBy:     true,
			RelationSimilarTo:  true,
		},
	}

	// Compile suspicious patterns for injection detection
	// Security: Detect potential prompt injection or malicious content
	patterns := []string{
		`(?i)ignore\s+(previous|all|above)`,
		`(?i)disregard\s+(the|all|previous)`,
		`(?i)forget\s+(everything|all|previous)`,
		`(?i)<script`,
		`(?i)javascript:`,
		`(?i)on(error|load|click)=`,
		`(?i)eval\s*\(`,
		`(?i)exec\s*\(`,
		`(?i)system\s*\(`,
		`(?i)__proto__`,
		`(?i)constructor\s*\[`,
	}

	v.suspiciousPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			v.suspiciousPatterns = append(v.suspiciousPatterns, re)
		}
	}

	return v
}

// ValidateAndConvertEntity validates an extracted entity and converts it to a graph entity.
// Returns nil if the entity is invalid or suspicious.
func (v *ExtractionValidator) ValidateAndConvertEntity(extracted ExtractedEntity, chunkID, documentID string) *Entity {
	// Check basic validation
	if err := extracted.Validate(); err != nil {
		return nil
	}

	// Check confidence threshold
	if extracted.Confidence < v.minConfidence {
		return nil
	}

	// Normalize and validate name
	name := v.normalizeName(extracted.Name)
	if name == "" || len(name) > v.maxNameLength {
		return nil
	}

	// Check for suspicious content
	if v.containsSuspiciousContent(name) || v.containsSuspiciousContent(extracted.Description) {
		return nil
	}

	// Validate entity type
	entityType := EntityType(extracted.Type)
	if !v.allowedEntityTypes[entityType] {
		entityType = EntityTypeConcept // Default to concept
	}

	// Normalize description
	description := v.normalizeDescription(extracted.Description)

	// Generate deterministic ID
	entityID := GenerateEntityID(name, string(entityType), documentID)

	return &Entity{
		ID:               entityID,
		Name:             name,
		Type:             entityType,
		Description:      description,
		Confidence:       extracted.Confidence,
		SourceChunkID:    chunkID,
		SourceDocumentID: documentID,
		Metadata:         make(map[string]interface{}),
	}
}

// ValidateAndConvertRelation validates an extracted relation and converts it to a graph relation.
// Returns nil if the relation is invalid or references unknown entities.
func (v *ExtractionValidator) ValidateAndConvertRelation(extracted ExtractedRelation, chunkID string, entities []Entity) *Relation {
	// Check basic validation
	if err := extracted.Validate(); err != nil {
		return nil
	}

	// Check confidence threshold
	if extracted.Confidence < v.minConfidence {
		return nil
	}

	// Normalize subject and object names
	subjectName := v.normalizeName(extracted.Subject)
	objectName := v.normalizeName(extracted.Object)

	if subjectName == "" || objectName == "" {
		return nil
	}

	// Check for suspicious content
	if v.containsSuspiciousContent(subjectName) || v.containsSuspiciousContent(objectName) {
		return nil
	}

	// Find matching entities by name
	var subjectID, objectID string
	for _, e := range entities {
		normalizedEntityName := v.normalizeName(e.Name)
		if normalizedEntityName == subjectName {
			subjectID = e.ID
		}
		if normalizedEntityName == objectName {
			objectID = e.ID
		}
	}

	// Both entities must exist
	if subjectID == "" || objectID == "" {
		return nil
	}

	// Self-referential relations are usually errors
	if subjectID == objectID {
		return nil
	}

	// Validate relation type
	relationType := RelationType(extracted.Predicate)
	if !v.allowedRelationTypes[relationType] {
		relationType = RelationRelatesTo // Default
	}

	// Generate deterministic ID
	relationID := GenerateRelationID(subjectID, string(relationType), objectID)

	return &Relation{
		ID:            relationID,
		SubjectID:     subjectID,
		SubjectName:   subjectName,
		Predicate:     relationType,
		ObjectID:      objectID,
		ObjectName:    objectName,
		Confidence:    extracted.Confidence,
		SourceChunkID: chunkID,
		Metadata:      make(map[string]interface{}),
	}
}

// normalizeName cleans and normalizes an entity name.
func (v *ExtractionValidator) normalizeName(name string) string {
	// Trim whitespace
	name = strings.TrimSpace(name)

	// Remove control characters
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)

	// Collapse multiple spaces
	name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")

	// Remove leading/trailing punctuation (but keep internal)
	name = strings.Trim(name, ".,;:!?\"'()[]{}<>")

	// Truncate if too long
	if len(name) > v.maxNameLength {
		name = name[:v.maxNameLength]
	}

	return name
}

// normalizeDescription cleans and normalizes a description.
func (v *ExtractionValidator) normalizeDescription(desc string) string {
	// Trim whitespace
	desc = strings.TrimSpace(desc)

	// Remove control characters except newlines
	desc = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, desc)

	// Truncate if too long
	if len(desc) > v.maxDescriptionLength {
		desc = desc[:v.maxDescriptionLength] + "..."
	}

	return desc
}

// containsSuspiciousContent checks for potential injection or malicious patterns.
// Security: Prevents storing potentially harmful content in the knowledge graph.
func (v *ExtractionValidator) containsSuspiciousContent(text string) bool {
	if text == "" {
		return false
	}

	for _, re := range v.suspiciousPatterns {
		if re.MatchString(text) {
			return true
		}
	}

	return false
}

// ValidateBatch validates a batch of extracted entities and relations.
// Returns only the valid items.
func (v *ExtractionValidator) ValidateBatch(
	extractedEntities []ExtractedEntity,
	extractedRelations []ExtractedRelation,
	chunkID, documentID string,
) ([]Entity, []Relation) {
	// First pass: validate entities
	validEntities := make([]Entity, 0, len(extractedEntities))
	for _, extracted := range extractedEntities {
		if entity := v.ValidateAndConvertEntity(extracted, chunkID, documentID); entity != nil {
			validEntities = append(validEntities, *entity)
		}
	}

	// Second pass: validate relations (requires entity list)
	validRelations := make([]Relation, 0, len(extractedRelations))
	for _, extracted := range extractedRelations {
		if relation := v.ValidateAndConvertRelation(extracted, chunkID, validEntities); relation != nil {
			validRelations = append(validRelations, *relation)
		}
	}

	return validEntities, validRelations
}

// SetMinConfidence updates the minimum confidence threshold.
func (v *ExtractionValidator) SetMinConfidence(threshold float64) {
	if threshold >= 0 && threshold <= 1 {
		v.minConfidence = threshold
	}
}

// GetStats returns validation statistics for monitoring.
func (v *ExtractionValidator) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"min_confidence":         v.minConfidence,
		"max_name_length":        v.maxNameLength,
		"max_description_length": v.maxDescriptionLength,
		"allowed_entity_types":   len(v.allowedEntityTypes),
		"allowed_relation_types": len(v.allowedRelationTypes),
		"suspicious_patterns":    len(v.suspiciousPatterns),
	}
}

// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// graph_schema.go defines entity and relation types for the knowledge graph.
package kb

import (
	"encoding/json"
	"time"
)

// EntityType represents the type of an entity in the knowledge graph.
type EntityType string

const (
	// EntityTypeConcept represents abstract concepts, ideas, or topics
	EntityTypeConcept EntityType = "concept"
	// EntityTypeOrganization represents companies, institutions, or groups
	EntityTypeOrganization EntityType = "organization"
	// EntityTypePerson represents individuals
	EntityTypePerson EntityType = "person"
	// EntityTypeSection represents document sections or headings
	EntityTypeSection EntityType = "section"
	// EntityTypeDocument represents source documents
	EntityTypeDocument EntityType = "document"
	// EntityTypeTechnology represents technical tools, frameworks, or protocols
	EntityTypeTechnology EntityType = "technology"
	// EntityTypeLocation represents geographic locations
	EntityTypeLocation EntityType = "location"
	// EntityTypeEvent represents events or incidents
	EntityTypeEvent EntityType = "event"
)

// ValidEntityTypes returns all valid entity types for validation.
func ValidEntityTypes() []EntityType {
	return []EntityType{
		EntityTypeConcept,
		EntityTypeOrganization,
		EntityTypePerson,
		EntityTypeSection,
		EntityTypeDocument,
		EntityTypeTechnology,
		EntityTypeLocation,
		EntityTypeEvent,
	}
}

// IsValidEntityType checks if an entity type is valid.
func IsValidEntityType(t EntityType) bool {
	for _, valid := range ValidEntityTypes() {
		if t == valid {
			return true
		}
	}
	return false
}

// RelationType represents the type of relationship between entities.
type RelationType string

const (
	// RelationMentions indicates an entity mentions another
	RelationMentions RelationType = "mentions"
	// RelationDefines indicates an entity defines another
	RelationDefines RelationType = "defines"
	// RelationRelatesTo indicates a general relationship
	RelationRelatesTo RelationType = "relates_to"
	// RelationContains indicates containment (e.g., section contains concept)
	RelationContains RelationType = "contains"
	// RelationPartOf indicates part-whole relationship
	RelationPartOf RelationType = "part_of"
	// RelationImplements indicates implementation relationship
	RelationImplements RelationType = "implements"
	// RelationDependsOn indicates dependency relationship
	RelationDependsOn RelationType = "depends_on"
	// RelationCreatedBy indicates authorship or creation
	RelationCreatedBy RelationType = "created_by"
	// RelationUsedBy indicates usage relationship
	RelationUsedBy RelationType = "used_by"
	// RelationSimilarTo indicates similarity between entities
	RelationSimilarTo RelationType = "similar_to"
)

// ValidRelationTypes returns all valid relation types for validation.
func ValidRelationTypes() []RelationType {
	return []RelationType{
		RelationMentions,
		RelationDefines,
		RelationRelatesTo,
		RelationContains,
		RelationPartOf,
		RelationImplements,
		RelationDependsOn,
		RelationCreatedBy,
		RelationUsedBy,
		RelationSimilarTo,
	}
}

// IsValidRelationType checks if a relation type is valid.
func IsValidRelationType(t RelationType) bool {
	for _, valid := range ValidRelationTypes() {
		if t == valid {
			return true
		}
	}
	return false
}

// Entity represents an entity in the knowledge graph.
type Entity struct {
	// ID is the unique identifier for the entity
	ID string `json:"id"`

	// Name is the display name of the entity
	Name string `json:"name"`

	// Type is the entity type (concept, person, organization, etc.)
	Type EntityType `json:"type"`

	// Description provides additional context about the entity
	Description string `json:"description,omitempty"`

	// SourceChunkID is the chunk this entity was extracted from
	SourceChunkID string `json:"source_chunk_id,omitempty"`

	// SourceDocumentID is the document this entity belongs to
	SourceDocumentID string `json:"source_document_id,omitempty"`

	// Metadata contains additional structured data
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Confidence is the extraction confidence score (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// CreatedAt is when the entity was first created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the entity was last updated
	UpdatedAt time.Time `json:"updated_at"`
}

// Relation represents a relationship between two entities.
type Relation struct {
	// ID is the unique identifier for the relation
	ID string `json:"id"`

	// SubjectID is the source entity ID
	SubjectID string `json:"subject_id"`

	// SubjectName is the source entity name (for display)
	SubjectName string `json:"subject_name,omitempty"`

	// Predicate is the relationship type
	Predicate RelationType `json:"predicate"`

	// ObjectID is the target entity ID
	ObjectID string `json:"object_id"`

	// ObjectName is the target entity name (for display)
	ObjectName string `json:"object_name,omitempty"`

	// SourceChunkID is the chunk this relation was extracted from
	SourceChunkID string `json:"source_chunk_id,omitempty"`

	// Confidence is the extraction confidence score (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// Metadata contains additional structured data
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// CreatedAt is when the relation was first created
	CreatedAt time.Time `json:"created_at"`
}

// ExtractionResult represents the result of entity/relation extraction from a chunk.
type ExtractionResult struct {
	// ChunkID is the source chunk ID
	ChunkID string `json:"chunk_id"`

	// DocumentID is the source document ID
	DocumentID string `json:"document_id"`

	// Entities are the extracted entities
	Entities []Entity `json:"entities"`

	// Relations are the extracted relations
	Relations []Relation `json:"relations"`

	// ProcessingTimeMs is the time taken to extract in milliseconds
	ProcessingTimeMs int64 `json:"processing_time_ms"`

	// Error contains any extraction error message
	Error string `json:"error,omitempty"`
}

// GraphStats contains statistics about the knowledge graph.
type GraphStats struct {
	// TotalEntities is the total number of entities
	TotalEntities int64 `json:"total_entities"`

	// TotalRelations is the total number of relations
	TotalRelations int64 `json:"total_relations"`

	// EntitiesByType is a breakdown by entity type
	EntitiesByType map[EntityType]int64 `json:"entities_by_type"`

	// RelationsByType is a breakdown by relation type
	RelationsByType map[RelationType]int64 `json:"relations_by_type"`

	// DocumentsCovered is the number of documents with entities
	DocumentsCovered int64 `json:"documents_covered"`

	// ChunksProcessed is the number of chunks processed
	ChunksProcessed int64 `json:"chunks_processed"`

	// LastUpdated is when the graph was last updated
	LastUpdated time.Time `json:"last_updated"`
}

// ToJSON serializes the entity to JSON.
func (e *Entity) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ToJSON serializes the relation to JSON.
func (r *Relation) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// EntityFromJSON deserializes an entity from JSON.
func EntityFromJSON(data []byte) (*Entity, error) {
	var e Entity
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// RelationFromJSON deserializes a relation from JSON.
func RelationFromJSON(data []byte) (*Relation, error) {
	var r Relation
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SecurityConstants for KAG input validation
const (
	// MaxEntityNameLength is the maximum length for entity names
	MaxEntityNameLength = 500

	// MaxDescriptionLength is the maximum length for descriptions
	MaxDescriptionLength = 2000

	// MaxQueryLength is the maximum length for KAG queries
	MaxQueryLength = 10240

	// MaxEntitiesPerChunk is the maximum entities extracted from one chunk
	MaxEntitiesPerChunk = 50

	// MaxRelationsPerChunk is the maximum relations extracted from one chunk
	MaxRelationsPerChunk = 100

	// MaxEntitiesHint is the maximum entity hints in a query
	MaxEntitiesHint = 50

	// MaxHops is the maximum graph traversal depth
	MaxHops = 5

	// MinConfidenceThreshold is the minimum allowed confidence threshold
	MinConfidenceThreshold = 0.0

	// MaxConfidenceThreshold is the maximum allowed confidence threshold
	MaxConfidenceThreshold = 1.0
)

// ValidateEntity validates an entity for security and integrity.
func ValidateEntity(e *Entity) error {
	if e.Name == "" {
		return ErrEmptyEntityName
	}
	if len(e.Name) > MaxEntityNameLength {
		return ErrEntityNameTooLong
	}
	if len(e.Description) > MaxDescriptionLength {
		return ErrDescriptionTooLong
	}
	if !IsValidEntityType(e.Type) {
		return ErrInvalidEntityType
	}
	if e.Confidence < MinConfidenceThreshold || e.Confidence > MaxConfidenceThreshold {
		return ErrInvalidConfidence
	}
	return nil
}

// ValidateRelation validates a relation for security and integrity.
func ValidateRelation(r *Relation) error {
	if r.SubjectID == "" {
		return ErrEmptySubjectID
	}
	if r.ObjectID == "" {
		return ErrEmptyObjectID
	}
	if !IsValidRelationType(r.Predicate) {
		return ErrInvalidRelationType
	}
	if r.Confidence < MinConfidenceThreshold || r.Confidence > MaxConfidenceThreshold {
		return ErrInvalidConfidence
	}
	return nil
}

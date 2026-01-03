// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// kag_errors.go defines error types for KAG operations.
package kb

import "errors"

// Entity validation errors
var (
	// ErrEmptyEntityName is returned when an entity has no name
	ErrEmptyEntityName = errors.New("entity name cannot be empty")

	// ErrEntityNameTooLong is returned when entity name exceeds maximum length
	ErrEntityNameTooLong = errors.New("entity name exceeds maximum length")

	// ErrDescriptionTooLong is returned when description exceeds maximum length
	ErrDescriptionTooLong = errors.New("description exceeds maximum length")

	// ErrInvalidEntityType is returned when entity type is not recognized
	ErrInvalidEntityType = errors.New("invalid entity type")

	// ErrInvalidConfidence is returned when confidence is outside 0.0-1.0 range
	ErrInvalidConfidence = errors.New("confidence must be between 0.0 and 1.0")
)

// Relation validation errors
var (
	// ErrEmptySubjectID is returned when relation has no subject
	ErrEmptySubjectID = errors.New("relation subject ID cannot be empty")

	// ErrEmptyObjectID is returned when relation has no object
	ErrEmptyObjectID = errors.New("relation object ID cannot be empty")

	// ErrInvalidRelationType is returned when relation type is not recognized
	ErrInvalidRelationType = errors.New("invalid relation type")

	// ErrSelfRelation is returned when subject and object are the same
	ErrSelfRelation = errors.New("entity cannot have a relation to itself")
)

// Query validation errors
var (
	// ErrEmptyQuery is returned when query string is empty
	ErrEmptyQuery = errors.New("query cannot be empty")

	// ErrQueryTooLong is returned when query exceeds maximum length
	ErrQueryTooLong = errors.New("query exceeds maximum length")

	// ErrTooManyEntityHints is returned when too many entity hints provided
	ErrTooManyEntityHints = errors.New("too many entity hints provided")

	// ErrInvalidMaxHops is returned when max_hops is outside valid range
	ErrInvalidMaxHops = errors.New("max_hops must be between 1 and 5")
)

// Graph store errors
var (
	// ErrGraphNotConnected is returned when graph database is not connected
	ErrGraphNotConnected = errors.New("graph database is not connected")

	// ErrGraphConnectionFailed is returned when connection to graph database fails
	ErrGraphConnectionFailed = errors.New("failed to connect to graph database")

	// ErrEntityNotFound is returned when entity is not found
	ErrEntityNotFound = errors.New("entity not found")

	// ErrRelationNotFound is returned when relation is not found
	ErrRelationNotFound = errors.New("relation not found")

	// ErrDuplicateEntity is returned when entity already exists
	ErrDuplicateEntity = errors.New("entity already exists")

	// ErrGraphQueryFailed is returned when graph query execution fails
	ErrGraphQueryFailed = errors.New("graph query execution failed")
)

// Extraction errors
var (
	// ErrExtractionFailed is returned when entity extraction fails
	ErrExtractionFailed = errors.New("entity extraction failed")

	// ErrExtractionTimeout is returned when extraction times out
	ErrExtractionTimeout = errors.New("entity extraction timed out")

	// ErrLLMProviderNotAvailable is returned when LLM provider is not available
	ErrLLMProviderNotAvailable = errors.New("LLM provider is not available")

	// ErrInvalidExtractionResponse is returned when LLM returns invalid format
	ErrInvalidExtractionResponse = errors.New("invalid extraction response format")

	// ErrTooManyEntities is returned when extraction exceeds entity limit
	ErrTooManyEntities = errors.New("extraction returned too many entities")

	// ErrTooManyRelations is returned when extraction exceeds relation limit
	ErrTooManyRelations = errors.New("extraction returned too many relations")
)

// Configuration errors
var (
	// ErrKAGDisabled is returned when KAG is not enabled
	ErrKAGDisabled = errors.New("KAG is not enabled in configuration")

	// ErrInvalidGraphBackend is returned when graph backend is not recognized
	ErrInvalidGraphBackend = errors.New("invalid graph database backend")

	// ErrInvalidLLMProvider is returned when LLM provider is not recognized
	ErrInvalidLLMProvider = errors.New("invalid LLM provider")
)

// IsKAGError checks if an error is a KAG-specific error.
func IsKAGError(err error) bool {
	kagErrors := []error{
		ErrEmptyEntityName, ErrEntityNameTooLong, ErrDescriptionTooLong,
		ErrInvalidEntityType, ErrInvalidConfidence,
		ErrEmptySubjectID, ErrEmptyObjectID, ErrInvalidRelationType, ErrSelfRelation,
		ErrEmptyQuery, ErrQueryTooLong, ErrTooManyEntityHints, ErrInvalidMaxHops,
		ErrGraphNotConnected, ErrGraphConnectionFailed, ErrEntityNotFound,
		ErrRelationNotFound, ErrDuplicateEntity, ErrGraphQueryFailed,
		ErrExtractionFailed, ErrExtractionTimeout, ErrLLMProviderNotAvailable,
		ErrInvalidExtractionResponse, ErrTooManyEntities, ErrTooManyRelations,
		ErrKAGDisabled, ErrInvalidGraphBackend, ErrInvalidLLMProvider,
	}

	for _, kagErr := range kagErrors {
		if errors.Is(err, kagErr) {
			return true
		}
	}
	return false
}

// IsRetryableError checks if an error is retryable.
func IsRetryableError(err error) bool {
	retryable := []error{
		ErrGraphConnectionFailed,
		ErrExtractionTimeout,
		ErrLLMProviderNotAvailable,
	}

	for _, retryErr := range retryable {
		if errors.Is(err, retryErr) {
			return true
		}
	}
	return false
}

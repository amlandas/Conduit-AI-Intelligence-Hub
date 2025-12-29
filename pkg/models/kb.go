package models

import "time"

// KBSource represents a registered knowledge base source.
type KBSource struct {
	SourceID   string    `json:"source_id"`
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Type       string    `json:"type"` // "folder", "git"
	Patterns   []string  `json:"patterns,omitempty"`
	Excludes   []string  `json:"excludes,omitempty"`
	SyncMode   string    `json:"sync_mode"` // "watch", "manual", "scheduled"
	Status     string    `json:"status"`    // "active", "paused", "error", "syncing"
	LastSync   *time.Time `json:"last_sync,omitempty"`
	DocCount   int       `json:"doc_count"`
	ChunkCount int       `json:"chunk_count"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Error      string    `json:"error,omitempty"`
}

// KBDocument represents an indexed document in the knowledge base.
type KBDocument struct {
	DocumentID string            `json:"document_id"`
	SourceID   string            `json:"source_id"`
	Path       string            `json:"path"`
	Title      string            `json:"title,omitempty"`
	MimeType   string            `json:"mime_type"`
	Size       int64             `json:"size"`
	ModifiedAt time.Time         `json:"modified_at"`
	IndexedAt  time.Time         `json:"indexed_at"`
	Hash       string            `json:"hash"` // SHA256 of content
	Metadata   map[string]string `json:"metadata,omitempty"`
	ChunkCount int               `json:"chunk_count"`
}

// KBChunk represents a searchable chunk of a document.
type KBChunk struct {
	ChunkID    string            `json:"chunk_id"`
	DocumentID string            `json:"document_id"`
	Index      int               `json:"chunk_index"`
	Content    string            `json:"content"`
	StartChar  int               `json:"start_char"`
	EndChar    int               `json:"end_char"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// SearchResult contains the results of a KB search.
type SearchResult struct {
	Results    []SearchHit `json:"results"`
	TotalHits  int         `json:"total_hits"`
	Query      string      `json:"query"`
	SearchTime float64     `json:"search_time_ms"`
}

// SearchHit represents a single search result.
type SearchHit struct {
	DocumentID string            `json:"document_id"`
	ChunkID    string            `json:"chunk_id"`
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	Snippet    string            `json:"snippet"`
	Score      float64           `json:"score"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// SyncResult contains the result of a source sync operation.
type SyncResult struct {
	SourceID string        `json:"source_id"`
	Added    int           `json:"added"`
	Updated  int           `json:"updated"`
	Deleted  int           `json:"deleted"`
	Errors   []SyncError   `json:"errors,omitempty"`
	Duration time.Duration `json:"duration"`
}

// SyncError represents an error during sync.
type SyncError struct {
	Path    string `json:"path"`
	Error   string `json:"error"`
	Skipped bool   `json:"skipped"`
}

// AddSourceRequest is the request to add a new KB source.
type AddSourceRequest struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Patterns []string `json:"patterns,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	SyncMode string   `json:"sync_mode,omitempty"`
}

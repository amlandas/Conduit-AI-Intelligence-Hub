// Package kb provides knowledge base functionality for document indexing and search.
package kb

import (
	"time"
)

// Source represents a registered source folder.
type Source struct {
	SourceID   string    `json:"source_id"`
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`      // "folder", "git", "confluence" (V1)
	Patterns   []string  `json:"patterns"`  // ["*.md", "*.txt"]
	Excludes   []string  `json:"excludes"`  // ["node_modules", ".git"]
	SyncMode   string    `json:"sync_mode"` // "watch", "manual", "scheduled"
	Status     string    `json:"status"`    // "active", "paused", "error"
	LastSync   time.Time `json:"last_sync"`
	DocCount   int       `json:"doc_count"`
	ChunkCount int       `json:"chunk_count"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Error      string    `json:"error,omitempty"`
}

// AddSourceRequest contains parameters for adding a source.
type AddSourceRequest struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Patterns []string `json:"patterns,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	SyncMode string   `json:"sync_mode"`
}

// UpdateSourceRequest contains parameters for updating a source.
type UpdateSourceRequest struct {
	Name     string   `json:"name,omitempty"`
	Patterns []string `json:"patterns,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	SyncMode string   `json:"sync_mode,omitempty"`
	Status   string   `json:"status,omitempty"`
}

// SyncOptions configures sync behavior.
type SyncOptions struct {
	RebuildVectors bool // Force regeneration of vector embeddings for all documents
}

// SyncResult contains the result of a sync operation.
type SyncResult struct {
	Added           int           `json:"added"`
	Updated         int           `json:"updated"`
	Deleted         int           `json:"deleted"`
	Errors          []SyncError   `json:"errors,omitempty"`
	Duration        time.Duration `json:"duration"`
	SemanticEnabled bool          `json:"semantic_enabled"` // Whether semantic indexing was attempted
	SemanticErrors  int           `json:"semantic_errors"`  // Number of documents with semantic indexing failures

	// ModelMismatch explains a semantic failure that no retry will fix: the
	// stored vectors were built by a different embedding model, so writing more
	// into the same index is refused. Empty in every other case. Additive field,
	// safe for existing consumers of this JSON.
	ModelMismatch string `json:"model_mismatch,omitempty"`
}

// SyncError represents an error during sync.
type SyncError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Document represents an indexed document.
type Document struct {
	DocumentID string            `json:"document_id"`
	SourceID   string            `json:"source_id"`
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	MimeType   string            `json:"mime_type"`
	Size       int64             `json:"size"`
	ModifiedAt time.Time         `json:"modified_at"`
	IndexedAt  time.Time         `json:"indexed_at"`
	Hash       string            `json:"hash"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ChunkCount int               `json:"chunk_count"`
}

// Chunk represents a searchable chunk of a document.
type Chunk struct {
	ChunkID   string            `json:"chunk_id"`
	Index     int               `json:"index"`
	Content   string            `json:"content"`
	StartChar int               `json:"start_char"`
	EndChar   int               `json:"end_char"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ChunkOptions configures chunking behavior.
type ChunkOptions struct {
	MaxSize   int      // Max chunk size in characters
	Overlap   int      // Overlap between chunks
	Splitters []string // Priority-ordered split points

	// DocumentID identifies the document being chunked. It is mixed into every
	// generated ChunkID (see ChunkID), which is what keeps a paragraph that
	// appears verbatim in two documents from producing one shared id.
	//
	// Leaving it empty is legal -- ids stay unique within the document -- but
	// any caller that persists chunks must set it. Indexer.Index does.
	DocumentID string
}

// FileMetadata contains file metadata for indexing.
type FileMetadata struct {
	Title      string
	MimeType   string
	Size       int64
	ModifiedAt time.Time
	Extra      map[string]string
}

// SearchResult contains search results.
type SearchResult struct {
	Results    []SearchHit `json:"results"`
	TotalHits  int         `json:"total_hits"`
	Query      string      `json:"query"`
	SearchTime float64     `json:"search_time_ms"`
}

// SearchHit represents a single search result.
type SearchHit struct {
	DocumentID string `json:"document_id"`
	ChunkID    string `json:"chunk_id"`
	Path       string `json:"path"`
	Title      string `json:"title"`
	Snippet    string `json:"snippet"`

	// Score is the relevance score. For every HybridSearcher mode it is on the
	// reciprocal-rank scale, 1/(k + rank) summed over contributing strategies:
	// positive, bounded, higher is better, comparable between modes. Searcher
	// (raw FTS5) still reports SQLite bm25, where more negative is better.
	Score float64 `json:"score"`

	// Rank is the 1-based position of this hit in the returned ordering.
	//
	// Issue #77: MMR runs last and reorders for diversity without touching
	// Score, so a client that sorted by Score got a DIFFERENT order than the
	// one Conduit returned, silently. Rank is the authoritative ordering.
	//
	// It is a separate field rather than a rewritten Score because collapsing
	// the two would throw away magnitude: a client can currently see that rank
	// 1 scored twice what rank 2 did, and forcing Score to be monotone in rank
	// would reduce that to "it came first".
	//
	// Zero means unranked -- Searcher and SemanticSearcher do not set it.
	Rank int `json:"rank,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`
}

// IngestionJob represents a job for the ingestion pipeline.
type IngestionJob struct {
	SourceID string
	FilePath string
	Action   string // "add", "update", "delete"
}

// Default patterns for common file types.
var DefaultPatterns = []string{
	// Documentation
	"*.md", "*.txt", "*.rst",
	// Code
	"*.go", "*.py", "*.js", "*.ts", "*.java", "*.rs", "*.rb",
	"*.c", "*.cpp", "*.h", "*.hpp", "*.cs", "*.swift", "*.kt",
	"*.sh", "*.bash", "*.zsh", "*.fish", "*.ps1", "*.bat", "*.cmd",
	// Config
	"*.json", "*.yaml", "*.yml", "*.xml", "*.jsonld", "*.toml", "*.ini", "*.cfg",
	// Data
	"*.csv", "*.tsv",
	// Documents (require external tools for text extraction)
	"*.pdf",
	"*.doc", "*.docx", "*.odt", "*.rtf",
}

// Default excludes for common directories.
var DefaultExcludes = []string{
	"node_modules", ".git", ".svn", ".hg",
	"__pycache__", ".pytest_cache",
	"vendor", "dist", "build", "target",
	".DS_Store", "Thumbs.db",
}

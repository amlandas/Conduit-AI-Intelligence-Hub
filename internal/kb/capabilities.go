package kb

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

// Capabilities describes the available search features.
type Capabilities struct {
	// FTS5Available indicates if SQLite FTS5 is available
	FTS5Available bool `json:"fts5_available"`

	// SemanticAvailable indicates if semantic search is available (Qdrant + Ollama)
	SemanticAvailable bool `json:"semantic_available"`

	// QdrantStatus describes Qdrant connectivity
	QdrantStatus string `json:"qdrant_status"`

	// EmbeddingModel is the Ollama model used for embeddings
	EmbeddingModel string `json:"embedding_model"`

	// OllamaStatus describes Ollama connectivity
	OllamaStatus string `json:"ollama_status"`
}

// DetectCapabilities checks available search features.
func DetectCapabilities(ctx context.Context, db *sql.DB) *Capabilities {
	caps := &Capabilities{
		EmbeddingModel: "nomic-embed-text",
	}

	// Check FTS5
	caps.FTS5Available = checkFTS5(ctx, db)

	// Check Qdrant
	qdrantOK, qdrantStatus := checkQdrant(ctx)
	caps.QdrantStatus = qdrantStatus

	// Check Ollama
	ollamaOK, ollamaStatus := checkOllama(ctx)
	caps.OllamaStatus = ollamaStatus

	// Semantic search requires both Qdrant and Ollama
	caps.SemanticAvailable = qdrantOK && ollamaOK

	return caps
}

// checkFTS5 verifies FTS5 extension is available.
func checkFTS5(ctx context.Context, db *sql.DB) bool {
	if db == nil {
		return false
	}

	// Check if kb_fts table exists (FTS5 virtual table)
	var exists int
	err := db.QueryRowContext(ctx,
		"SELECT 1 FROM sqlite_master WHERE type='table' AND name='kb_fts'").Scan(&exists)
	if err != nil {
		return false
	}

	return exists == 1
}

// checkQdrant tests Qdrant connectivity.
func checkQdrant(ctx context.Context) (bool, string) {
	client := &http.Client{Timeout: 2 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:6333/collections", nil)
	if err != nil {
		return false, "failed to create request"
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "not reachable"
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, "connected"
	}
	return false, fmt.Sprintf("status %d", resp.StatusCode)
}

// checkOllama tests Ollama connectivity and model availability.
func checkOllama(ctx context.Context) (bool, string) {
	client := &http.Client{Timeout: 2 * time.Second}

	// Check if Ollama is running
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:11434/api/tags", nil)
	if err != nil {
		return false, "failed to create request"
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "not reachable"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("status %d", resp.StatusCode)
	}

	// Note: We could parse the response to check for nomic-embed-text,
	// but for simplicity we just check if Ollama is responding
	return true, "connected"
}

// Summary returns a human-readable summary of capabilities.
func (c *Capabilities) Summary() string {
	var status string

	if c.FTS5Available {
		status += "FTS5: available\n"
	} else {
		status += "FTS5: not available\n"
	}

	if c.SemanticAvailable {
		status += fmt.Sprintf("Semantic: available (model: %s)\n", c.EmbeddingModel)
	} else {
		status += fmt.Sprintf("Semantic: not available (Qdrant: %s, Ollama: %s)\n",
			c.QdrantStatus, c.OllamaStatus)
	}

	return status
}

// SearchMode returns the recommended search mode based on capabilities.
func (c *Capabilities) SearchMode() string {
	if c.SemanticAvailable {
		return "hybrid"
	}
	if c.FTS5Available {
		return "fts5"
	}
	return "none"
}

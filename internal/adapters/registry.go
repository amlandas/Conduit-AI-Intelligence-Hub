package adapters

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// registry implements the Registry interface.
type registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry creates a new empty adapter registry.
func NewRegistry() Registry {
	return &registry{
		adapters: make(map[string]Adapter),
	}
}

// Register adds an adapter to the registry.
func (r *registry) Register(adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.ID()] = adapter
}

// Get retrieves an adapter by client ID.
func (r *registry) Get(clientID string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	adapter, ok := r.adapters[clientID]
	if !ok {
		return nil, fmt.Errorf("adapter not found: %s", clientID)
	}
	return adapter, nil
}

// List returns all registered adapters.
func (r *registry) List() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()

	adapters := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		adapters = append(adapters, a)
	}
	return adapters
}

// DetectAll runs detection on all registered adapters.
func (r *registry) DetectAll(ctx context.Context) (map[string]*DetectResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]*DetectResult)
	for id, adapter := range r.adapters {
		result, err := adapter.Detect(ctx)
		if err != nil {
			result = &DetectResult{Installed: false, Notes: err.Error()}
		}
		results[id] = result
	}
	return results, nil
}

// DefaultRegistry creates a registry with all built-in adapters.
func DefaultRegistry(db *sql.DB) Registry {
	r := NewRegistry()
	r.Register(NewClaudeCodeAdapter(db))
	r.Register(NewCursorAdapter(db))
	r.Register(NewVSCodeAdapter(db))
	r.Register(NewGeminiCLIAdapter(db))
	return r
}

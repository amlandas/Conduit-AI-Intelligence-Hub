// Package daemon implements the Conduit daemon core.
package daemon

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// EventType represents the type of event being published.
type EventType string

// Event types for different categories of changes.
const (
	// Instance events
	EventInstanceCreated       EventType = "instance_created"
	EventInstanceDeleted       EventType = "instance_deleted"
	EventInstanceStatusChanged EventType = "instance_status_changed"
	EventInstanceHealthChanged EventType = "instance_health_changed"

	// KB events
	EventKBSourceAdded     EventType = "kb_source_added"
	EventKBSourceRemoved   EventType = "kb_source_removed"
	EventKBSyncStarted     EventType = "kb_sync_started"
	EventKBSyncProgress    EventType = "kb_sync_progress"
	EventKBSyncCompleted   EventType = "kb_sync_completed"
	EventKBSyncFailed      EventType = "kb_sync_failed"
	EventKBMigrateStarted  EventType = "kb_migrate_started"
	EventKBMigrateProgress EventType = "kb_migrate_progress"
	EventKBMigrateComplete EventType = "kb_migrate_completed"

	// KAG events
	EventKAGExtractionStarted  EventType = "kag_extraction_started"
	EventKAGExtractionProgress EventType = "kag_extraction_progress"
	EventKAGExtractionComplete EventType = "kag_extraction_completed"

	// Binding events
	EventBindingCreated EventType = "binding_created"
	EventBindingDeleted EventType = "binding_deleted"

	// System events
	EventDaemonStatus       EventType = "daemon_status"
	EventDependencyStatus   EventType = "dependency_status"
	EventQdrantStatusChange EventType = "qdrant_status_changed"
)

// Event represents a single event published by the daemon.
type Event struct {
	ID        uint64          `json:"id"`
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// EventBus manages event subscriptions and publishing.
// It is thread-safe and designed for SSE broadcasting.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan *Event
	nextID      uint64
	eventID     atomic.Uint64
	bufferSize  int
	closed      bool
}

// NewEventBus creates a new EventBus with the given channel buffer size.
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 100 // Default buffer
	}
	return &EventBus{
		subscribers: make(map[uint64]chan *Event),
		bufferSize:  bufferSize,
	}
}

// Subscribe creates a new subscription and returns a channel for receiving events.
// The returned ID should be used to Unsubscribe when done.
func (eb *EventBus) Subscribe() (uint64, <-chan *Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return 0, nil
	}

	id := eb.nextID
	eb.nextID++

	ch := make(chan *Event, eb.bufferSize)
	eb.subscribers[id] = ch

	return id, ch
}

// Unsubscribe removes a subscription and closes its channel.
func (eb *EventBus) Unsubscribe(id uint64) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if ch, ok := eb.subscribers[id]; ok {
		close(ch)
		delete(eb.subscribers, id)
	}
}

// Publish broadcasts an event to all subscribers.
// If a subscriber's channel is full, the event is dropped for that subscriber.
func (eb *EventBus) Publish(eventType EventType, data interface{}) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	event := &Event{
		ID:        eb.eventID.Add(1),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      dataBytes,
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return nil
	}

	for _, ch := range eb.subscribers {
		select {
		case ch <- event:
			// Sent successfully
		default:
			// Channel full, drop event for this subscriber
			// This prevents slow subscribers from blocking others
		}
	}

	return nil
}

// PublishRaw broadcasts a pre-marshaled event to all subscribers.
func (eb *EventBus) PublishRaw(eventType EventType, data json.RawMessage) {
	event := &Event{
		ID:        eb.eventID.Add(1),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return
	}

	for _, ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

// SubscriberCount returns the current number of active subscribers.
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers)
}

// Close closes the EventBus and all subscriber channels.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return
	}

	eb.closed = true
	for id, ch := range eb.subscribers {
		close(ch)
		delete(eb.subscribers, id)
	}
}

// Event data structures for typed events

// InstanceStatusData contains data for instance status change events.
type InstanceStatusData struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	PrevStatus string `json:"prev_status,omitempty"`
}

// InstanceHealthData contains data for instance health change events.
type InstanceHealthData struct {
	InstanceID string `json:"instance_id"`
	Healthy    bool   `json:"healthy"`
	Message    string `json:"message,omitempty"`
}

// KBSourceData contains data for KB source events.
type KBSourceData struct {
	SourceID string `json:"source_id"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
}

// KBSyncProgressData contains data for sync progress events.
type KBSyncProgressData struct {
	SourceID   string  `json:"source_id"`
	Current    int     `json:"current"`
	Total      int     `json:"total"`
	Percentage float64 `json:"percentage"`
	Phase      string  `json:"phase"` // "scanning", "indexing", "vectorizing"
}

// KBSyncResultData contains data for sync completion events.
type KBSyncResultData struct {
	SourceID     string `json:"source_id"`
	Added        int    `json:"added"`
	Updated      int    `json:"updated"`
	Deleted      int    `json:"deleted"`
	Errors       int    `json:"errors"`
	Duration     string `json:"duration"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// KAGProgressData contains data for KAG extraction progress events.
type KAGProgressData struct {
	SourceID   string  `json:"source_id,omitempty"`
	Current    int     `json:"current"`
	Total      int     `json:"total"`
	Percentage float64 `json:"percentage"`
	Entities   int     `json:"entities"`
	Relations  int     `json:"relations"`
}

// BindingData contains data for binding events.
type BindingData struct {
	BindingID  string `json:"binding_id"`
	InstanceID string `json:"instance_id"`
	ClientID   string `json:"client_id"`
	Scope      string `json:"scope"`
}

// DaemonStatusData contains data for daemon heartbeat events.
type DaemonStatusData struct {
	Status      string    `json:"status"` // "running", "shutting_down"
	Uptime      string    `json:"uptime"`
	StartTime   time.Time `json:"start_time"`
	Subscribers int       `json:"subscribers"`
}

// DependencyStatusData contains data for dependency status events.
type DependencyStatusData struct {
	Name      string `json:"name"` // "qdrant", "ollama", "falkordb", "container_runtime"
	Available bool   `json:"available"`
	Status    string `json:"status"` // "running", "stopped", "error", "not_installed"
	Details   string `json:"details,omitempty"`
}

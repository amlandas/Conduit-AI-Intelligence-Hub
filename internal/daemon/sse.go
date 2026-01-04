// Package daemon implements the Conduit daemon core.
package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleSSEEvents handles the SSE endpoint for real-time event streaming.
// GET /api/v1/events
//
// This endpoint streams events using Server-Sent Events (SSE) protocol.
// Clients should handle reconnection as the daemon may restart.
//
// Event format:
//
//	id: <event_id>
//	event: <event_type>
//	data: <json_payload>
func (d *Daemon) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	// Check if EventBus is initialized
	if d.eventBus == nil {
		http.Error(w, "event bus not available", http.StatusServiceUnavailable)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Ensure we can flush
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to events
	subID, eventCh := d.eventBus.Subscribe()
	if eventCh == nil {
		http.Error(w, "event bus closed", http.StatusServiceUnavailable)
		return
	}
	defer d.eventBus.Unsubscribe(subID)

	d.logger.Debug().
		Uint64("subscriber_id", subID).
		Msg("SSE client connected")

	// Send initial connection event
	if err := writeSSEEvent(w, flusher, &Event{
		ID:        0,
		Type:      "connected",
		Timestamp: time.Now(),
		Data:      json.RawMessage(`{"message":"connected to event stream"}`),
	}); err != nil {
		return
	}

	// Create heartbeat ticker (every 30 seconds)
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// Stream events until client disconnects or server shuts down
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			d.logger.Debug().
				Uint64("subscriber_id", subID).
				Msg("SSE client disconnected")
			return

		case <-d.shutdownCh:
			// Server shutting down
			writeSSEEvent(w, flusher, &Event{
				ID:        0,
				Type:      "shutdown",
				Timestamp: time.Now(),
				Data:      json.RawMessage(`{"message":"daemon shutting down"}`),
			})
			return

		case event, ok := <-eventCh:
			if !ok {
				// Channel closed
				return
			}
			if err := writeSSEEvent(w, flusher, event); err != nil {
				d.logger.Debug().
					Err(err).
					Uint64("subscriber_id", subID).
					Msg("failed to write SSE event")
				return
			}

		case <-heartbeat.C:
			// Send heartbeat to keep connection alive
			d.mu.RLock()
			uptime := time.Since(d.startTime).Truncate(time.Second).String()
			d.mu.RUnlock()

			heartbeatData := DaemonStatusData{
				Status:      "running",
				Uptime:      uptime,
				StartTime:   d.startTime,
				Subscribers: d.eventBus.SubscriberCount(),
			}
			dataBytes, _ := json.Marshal(heartbeatData)

			if err := writeSSEEvent(w, flusher, &Event{
				ID:        0,
				Type:      EventDaemonStatus,
				Timestamp: time.Now(),
				Data:      dataBytes,
			}); err != nil {
				return
			}
		}
	}
}

// writeSSEEvent writes a single SSE event to the response writer.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event *Event) error {
	// SSE format:
	// id: <id>
	// event: <type>
	// data: <json>
	// <blank line>

	if event.ID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "data: %s\n\n", event.Data); err != nil {
		return err
	}

	flusher.Flush()
	return nil
}

// SSEStats returns current SSE connection statistics.
type SSEStats struct {
	Subscribers int  `json:"subscribers"`
	Available   bool `json:"available"`
}

// handleSSEStats returns SSE connection statistics.
// GET /api/v1/events/stats
func (d *Daemon) handleSSEStats(w http.ResponseWriter, r *http.Request) {
	stats := SSEStats{
		Available: d.eventBus != nil,
	}
	if d.eventBus != nil {
		stats.Subscribers = d.eventBus.SubscriberCount()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

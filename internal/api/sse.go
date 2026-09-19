package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleRunEvents streams a run's log as Server-Sent Events — the WebUI's
// run log tails this directly rather than polling GET /api/runs/{id}.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	run, err := s.Runs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Replay what already happened before this client subscribed, so a tab
	// opened mid-run isn't missing the start of the log.
	for _, entry := range run.LogEntries() {
		writeSSEEvent(w, entry)
	}
	flusher.Flush()

	ch := run.Subscribe()
	defer run.Unsubscribe(ch)
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, entry)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleRunsEvents streams the runs list as Server-Sent Events: an initial
// snapshot (the same rows and order GET /api/runs returns), then one delta
// per run each time something the list renders about it changes — see
// RunStore.SubscribeChanges and Run.notifyChanged's call sites. Replaces
// polling GET /api/runs's ETag; see docs/runs.md for the measurement this
// change is conditioned on.
func (s *Server) handleRunsEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe before snapshotting, not after: a change that lands in the
	// gap between "read the list" and "start listening" would otherwise
	// never reach this client at all.
	ch := s.Runs.SubscribeChanges()
	defer s.Runs.UnsubscribeChanges(ch)

	snapshot := make([]any, 0)
	for _, run := range sortedRuns(s.Runs) {
		snapshot = append(snapshot, runSummaryToJSON(run))
	}
	writeSSEEvent(w, map[string]any{"type": "snapshot", "runs": snapshot})
	flusher.Flush()

	for {
		select {
		case id, ok := <-ch:
			if !ok {
				return
			}
			run, err := s.Runs.Get(id)
			if err != nil {
				continue // gone from the in-memory store between the change and here
			}
			writeSSEEvent(w, map[string]any{"type": "run", "run": runSummaryToJSON(run)})
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

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

func writeSSEEvent(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

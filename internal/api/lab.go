// Lab's REST+SSE surface — one ongoing conversation, streamed the same
// way a swarm run or a foundry job already is (see sse.go, foundry.go).
// internal/lab.Session embeds a *runner.Run for exactly that reason: one
// pub-sub mechanism for the whole app, not a second one built for chat.
package api

import (
	"context"
	"fmt"
	"net/http"
)

type labMessageRequest struct {
	Message string `json:"message"`
}

// handleLabMessage takes one chat message and returns immediately —
// internal/lab.Session.HandleMessage runs in the background and streams
// its progress onto GET /api/lab/events, the same "kick it off, then
// subscribe" shape POST /api/runs already uses.
func (s *Server) handleLabMessage(w http.ResponseWriter, r *http.Request) {
	if s.Lab == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("Lab isn't configured on this server"))
		return
	}
	var req labMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("message is required"))
		return
	}
	// context.Background(), not r.Context(): this handler returns almost
	// immediately (202, then the client watches SSE instead), and net/http
	// cancels a request's context the moment its handler returns. Passing
	// r.Context() through to the goroutine — the first version of this did
	// exactly that — canceled the model call before it could ever answer,
	// every single time: "I couldn't decide what to do with that: context
	// canceled" on every message, found running this for real rather than
	// against a mocked generator, which never exercised the cancellation.
	go s.Lab.HandleMessage(context.Background(), req.Message)
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// handleLabEvents mirrors sse.go's handleRunEvents exactly, over Lab's one
// ongoing session — a tab opened mid-conversation replays everything said
// so far, then stays subscribed.
func (s *Server) handleLabEvents(w http.ResponseWriter, r *http.Request) {
	if s.Lab == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("Lab isn't configured on this server"))
		return
	}
	run := s.Lab.Run()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

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

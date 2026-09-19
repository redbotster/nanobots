package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
)

// sseReader reads one "data: ...\n\n" frame at a time from a real HTTP
// response — a streaming handler needs a real listener and a concurrent
// reader, not an httptest.ResponseRecorder, which only ever sees the whole
// buffered body after the handler returns.
type sseReader struct {
	t *testing.T
	r *bufio.Reader
}

// next blocks until one frame arrives or 2 seconds pass. A bare blocking
// read here would turn "the broadcast never happened" into a hung test
// binary rather than a failure — internal/runner/store_test.go hit exactly
// this before drainOne existed.
func (s *sseReader) next() map[string]any {
	s.t.Helper()
	ch := make(chan map[string]any, 1)
	go func() {
		for {
			line, err := s.r.ReadString('\n')
			if err != nil {
				return
			}
			if !strings.HasPrefix(line, "data: ") {
				continue // blank line between frames
			}
			var v map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &v) == nil {
				ch <- v
			}
			return
		}
	}()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		s.t.Fatal("no SSE frame arrived within 2s")
		return nil
	}
}

func connectRunsEvents(t *testing.T, srv *httptest.Server) *sseReader {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/runs/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	return &sseReader{t: t, r: bufio.NewReader(res.Body)}
}

// The whole point of this endpoint: a client that connects sees every run
// that already exists, then learns about a new one and a status change
// without a second request — nothing here is a poll.
func TestHandleRunsEventsStreamsASnapshotThenDeltas(t *testing.T) {
	store := runner.NewRunStore()
	existing := runner.NewRun("already-running")
	store.Add(existing)

	srv := httptest.NewServer((&Server{Runs: store}).Handler())
	t.Cleanup(srv.Close)

	stream := connectRunsEvents(t, srv)

	snapshot := stream.next()
	if snapshot["type"] != "snapshot" {
		t.Fatalf("first frame type = %v, want snapshot", snapshot["type"])
	}
	runs, ok := snapshot["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("snapshot runs = %#v, want exactly the one existing run", snapshot["runs"])
	}
	first := runs[0].(map[string]any)
	if first["id"] != existing.ID {
		t.Errorf("snapshot run id = %v, want %v", first["id"], existing.ID)
	}

	added := runner.NewRun("just-started")
	store.Add(added)

	delta := stream.next()
	if delta["type"] != "run" {
		t.Fatalf("delta type = %v, want run", delta["type"])
	}
	run := delta["run"].(map[string]any)
	if run["id"] != added.ID {
		t.Errorf("delta run id = %v, want %v (the new run, not the existing one)", run["id"], added.ID)
	}

	existing.SetStatus(runner.StatusRunning)
	delta = stream.next()
	run = delta["run"].(map[string]any)
	if run["id"] != existing.ID {
		t.Errorf("status-change delta id = %v, want %v", run["id"], existing.ID)
	}
	if run["status"] != string(runner.StatusRunning) {
		t.Errorf("status-change delta status = %v, want running", run["status"])
	}
}

// A run added between the snapshot being read and the subscription
// starting must not be lost — this is the ordering handleRunsEvents itself
// depends on (subscribe, then snapshot), proven from the client's side.
func TestHandleRunsEventsSubscribesBeforeSnapshotting(t *testing.T) {
	store := runner.NewRunStore()
	srv := httptest.NewServer((&Server{Runs: store}).Handler())
	t.Cleanup(srv.Close)

	stream := connectRunsEvents(t, srv)
	snapshot := stream.next()
	if runs := snapshot["runs"].([]any); len(runs) != 0 {
		t.Fatalf("snapshot = %v, want empty (nothing existed yet)", runs)
	}

	run := runner.NewRun("racing-the-subscribe")
	store.Add(run)

	delta := stream.next()
	if delta["run"].(map[string]any)["id"] != run.ID {
		t.Errorf("got %v, want the run just added", delta)
	}
}

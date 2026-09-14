package oneclaw

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// tokenServer stands in for 1Claw: it serves the api-key exchange every
// call needs, plus whatever the test adds.
func tokenServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	mux.HandleFunc("/v1/auth/api-key-token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token":"tok-1","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(srv *httptest.Server) *Client {
	c := NewClient("api-key")
	c.BaseURL = srv.URL
	return c
}

func TestOTelTopologyParsesTheGraph(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/otel/topology", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"nodes":[
			{"id":"agent:1","kind":"agent","label":"nanobots-inbox-triage","status":"ok","trust":100},
			{"id":"vault:1","kind":"vault","label":"__agent-keys"}],
			"edges":[{"from":"agent:1","to":"policy:1","kind":"holds"}],
			"truncated":false,"total_nodes":2}`)
	})
	got, err := clientFor(tokenServer(t, mux)).OTelTopology()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("nodes=%d edges=%d", len(got.Nodes), len(got.Edges))
	}
	// Trust is a pointer because a vault has none, and zero would read as
	// "completely untrusted" rather than "not a thing that has trust".
	if got.Nodes[0].Trust == nil || *got.Nodes[0].Trust != 100 {
		t.Errorf("agent trust = %v", got.Nodes[0].Trust)
	}
	if got.Nodes[1].Trust != nil {
		t.Errorf("a vault came back with a trust score: %v", *got.Nodes[1].Trust)
	}
}

func TestStreamOTelDeliversEachSignal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/otel/stream", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Last-Event-ID"); got != "41" {
			t.Errorf("Last-Event-ID = %q — a reconnect would replay or skip", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 42\ndata: {\"event_id\":42,\"kind\":\"span\",\"name\":\"oneclaw.policy.evaluate\",\"outcome\":\"allow\",\"duration_ms\":153}\n\n")
		fmt.Fprint(w, ": a comment frame the client must ignore\n\n")
		fmt.Fprint(w, "data: not json at all\n\n")
		fmt.Fprint(w, "id: 43\ndata: {\"event_id\":43,\"kind\":\"span\",\"name\":\"second\"}\n\n")
	})
	var got []Signal
	err := clientFor(tokenServer(t, mux)).StreamOTel(context.Background(), "41",
		func(s Signal) { got = append(got, s) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	// Two signals: the comment frame and the malformed one are skipped
	// without ending the stream, because one bad frame is not a reason to
	// stop watching.
	if len(got) != 2 {
		t.Fatalf("got %d signals, want 2: %+v", len(got), got)
	}
	if got[0].Outcome != "allow" || got[0].DurationM != 153 || got[1].EventID != 43 {
		t.Errorf("signals = %+v", got)
	}
}

// A caller that reconnects on every error will make a rate limit worse, so
// this one has to be identifiable rather than just another status in a
// string. Found for real: probing the endpoint a few times in a row while
// developing returned 429, and the first symptom looked like a hang.
func TestStreamOTelReportsRateLimitingDistinctly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/otel/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	err := clientFor(tokenServer(t, mux)).StreamOTel(context.Background(), "", func(Signal) {})
	if !errors.Is(err, ErrStreamRateLimited) {
		t.Fatalf("err = %v, want ErrStreamRateLimited", err)
	}
	if got := err.Error(); !contains(got, "60") {
		t.Errorf("the retry-after was dropped: %q", got)
	}
}

func TestStreamOTelStopsWhenTheContextIsCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/otel/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // hold it open like the real one does
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = clientFor(tokenServer(t, mux)).StreamOTel(ctx, "", func(Signal) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelling the context did not end the stream")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

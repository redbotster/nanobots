package slack

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	orig := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = orig })
	return NewClient("xoxb-test")
}

func TestPostMessageReturnsTimestamp(t *testing.T) {
	var gotAuth, gotChannel, gotText string
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		gotChannel, gotText = body["channel"], body["text"]
		json.NewEncoder(w).Encode(postMessageResponse{OK: true, TS: "1234.5678", Channel: "C123"})
	})
	c := testClient(t, mux)

	ts, err := c.PostMessage("#general", "hello team")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if ts != "1234.5678" {
		t.Errorf("ts = %q", ts)
	}
	if gotAuth != "Bearer xoxb-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotChannel != "#general" || gotText != "hello team" {
		t.Errorf("channel=%q text=%q", gotChannel, gotText)
	}
}

func TestPostMessageReturnsErrorOnSlackLevelFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		// Slack returns HTTP 200 even for a rejected request.
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(postMessageResponse{OK: false, Error: "channel_not_found"})
	})
	c := testClient(t, mux)

	if _, err := c.PostMessage("#nope", "hi"); err == nil {
		t.Fatal("expected an error when Slack's own ok field is false")
	}
}

func TestPostMessageReturnsErrorOnHTTPFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := testClient(t, mux)

	if _, err := c.PostMessage("#general", "hi"); err == nil {
		t.Fatal("expected an error for a 401 response")
	}
}

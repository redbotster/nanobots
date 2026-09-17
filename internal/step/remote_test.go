package step

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func TestRemoteDepsServiceCallRoundTrip(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["op"] != "messages.list" {
			t.Errorf("op = %v, want messages.list", body["op"])
		}
		json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{{"id": "1"}}})
	}))
	defer srv.Close()

	rd := NewRemoteDeps(srv.URL, "run-token-abc", nil)
	svc := schema.Service{ID: "gmail"}
	result, err := rd.ServiceCall(svc, "messages.list", map[string]any{"q": "x"})
	if err != nil {
		t.Fatalf("ServiceCall: %v", err)
	}
	if gotAuth != "Bearer run-token-abc" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotPath != "/internal/steps/service_call" {
		t.Errorf("path = %q", gotPath)
	}
	arr, ok := result.([]any)
	if !ok || len(arr) != 1 {
		t.Errorf("result = %#v", result)
	}
}

func TestRemoteDepsErrorResponsePropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"error": "no such service"})
	}))
	defer srv.Close()

	rd := NewRemoteDeps(srv.URL, "tok", nil)
	_, err := rd.ServiceCall(schema.Service{ID: "nope"}, "op", nil)
	if err == nil {
		t.Fatal("expected the callback error to propagate")
	}
	// Verbatim, with nothing in front of it. The daemon has already worded
	// this one for a person — "no connected account yet ... connect it from
	// Settings" — and it used to arrive on the Runs page behind `callback
	// /internal/steps/service_call:`, which is a path nobody can act on.
	if err.Error() != "no such service" {
		t.Errorf("the daemon's own message came back as %q", err)
	}
}

// The other errors in RemoteDeps.call are about the callback itself, and
// there the path is the only thing that says which one failed.
func TestACallbackThatNeverAnsweredNamesTheCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	rd := NewRemoteDeps(srv.URL, "tok", nil)
	err := rd.Notify("hello", "slack")
	if err == nil {
		t.Fatal("a 502 from nanobotd was treated as success")
	}
	if !strings.Contains(err.Error(), "/internal/steps/notify") {
		t.Errorf("a transport failure does not say which callback broke: %q", err)
	}
}

func TestRemoteDepsAIGenerateAndMemory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/steps/ai_generate", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": "hello"})
	})
	stored := ""
	mux.HandleFunc("/internal/steps/memory_put", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		stored = body["value"]
		json.NewEncoder(w).Encode(map[string]any{})
	})
	mux.HandleFunc("/internal/steps/memory_get", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"value": stored, "found": stored != ""}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rd := NewRemoteDeps(srv.URL, "tok", nil)
	text, err := rd.AIGenerate("prompt", schema.Model{})
	if err != nil || text != "hello" {
		t.Fatalf("AIGenerate = %q, %v", text, err)
	}
	if err := rd.MemoryPut("ns", "k", "v"); err != nil {
		t.Fatalf("MemoryPut: %v", err)
	}
	val, found, err := rd.MemoryGet("ns", "k")
	if err != nil || !found || val != "v" {
		t.Fatalf("MemoryGet = %q, %v, %v", val, found, err)
	}
}

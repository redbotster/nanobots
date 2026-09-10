package oneclaw

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMemoryGetMissingKeyReturnsNotFound(t *testing.T) {
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents/a1/memory/ns/k": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	val, ok, err := c.MemoryGet("a1", "ns", "k")
	if err != nil {
		t.Fatalf("MemoryGet: %v", err)
	}
	if ok || val != "" {
		t.Errorf("MemoryGet = %q, %v, want empty, false", val, ok)
	}
}

func TestMemoryPutThenGet(t *testing.T) {
	stored := ""
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents/a1/memory/ns/k": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)
				stored = body["value"]
				json.NewEncoder(w).Encode(map[string]string{"value": stored})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"value": stored})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL
	if err := c.MemoryPut("a1", "ns", "k", "2026-09-10T00:00:00Z"); err != nil {
		t.Fatalf("MemoryPut: %v", err)
	}
	val, ok, err := c.MemoryGet("a1", "ns", "k")
	if err != nil || !ok {
		t.Fatalf("MemoryGet: %v, ok=%v", err, ok)
	}
	if val != "2026-09-10T00:00:00Z" {
		t.Errorf("MemoryGet = %q", val)
	}
}

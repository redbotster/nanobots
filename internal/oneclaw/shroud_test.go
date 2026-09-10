package oneclaw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShroudChatSendsExpectedHeadersAndParsesResponse(t *testing.T) {
	var gotAgentKey, gotProvider string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgentKey = r.Header.Get("X-Shroud-Agent-Key")
		gotProvider = r.Header.Get("X-Shroud-Provider")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello back"}},
			},
		})
	}))
	defer srv.Close()

	c := NewShroudClient("agent-1", "ocv_key")
	c.BaseURL = srv.URL
	reply, err := c.Chat("anthropic", "claude-sonnet-4-6", "hi", 100)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply != "hello back" {
		t.Errorf("reply = %q, want %q", reply, "hello back")
	}
	if gotAgentKey != "agent-1:ocv_key" {
		t.Errorf("X-Shroud-Agent-Key = %q, want agent-1:ocv_key", gotAgentKey)
	}
	if gotProvider != "anthropic" {
		t.Errorf("X-Shroud-Provider = %q, want anthropic", gotProvider)
	}
	if gotBody["model"] != "claude-sonnet-4-6" {
		t.Errorf("body model = %v, want claude-sonnet-4-6", gotBody["model"])
	}
}

func TestShroudChatNoChoicesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	c := NewShroudClient("agent-1", "ocv_key")
	c.BaseURL = srv.URL
	if _, err := c.Chat("anthropic", "claude-sonnet-4-6", "hi", 100); err == nil {
		t.Fatal("expected an error for an empty choices response")
	}
}

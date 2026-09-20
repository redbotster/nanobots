package oneclaw

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestUpdateAgentPatchesMemoryEnabled(t *testing.T) {
	var gotBody map[string]any
	var gotMethod string
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents/a1": func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			json.NewDecoder(r.Body).Decode(&gotBody)
			json.NewEncoder(w).Encode(Agent{ID: "a1", Name: "nanobots-recap", MemoryEnabled: true})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL

	enabled := true
	agent, err := c.UpdateAgent("a1", UpdateAgentRequest{MemoryEnabled: &enabled})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotBody["memory_enabled"] != true {
		t.Errorf("request body memory_enabled = %v, want true", gotBody["memory_enabled"])
	}
	if !agent.MemoryEnabled {
		t.Error("expected the returned agent to report memory_enabled=true")
	}
}

// A live probe against a real agent found execution_intents_enabled off by
// default even with a connector installed and connected — POST
// /v1/agents/{id}/execute 403s with "Execution Intents are not enabled for
// this agent" before it ever looks at the binding name or intent_type. This
// is the field that turns it on.
func TestUpdateAgentPatchesExecutionIntentsEnabled(t *testing.T) {
	var gotBody map[string]any
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents/a1": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotBody)
			json.NewEncoder(w).Encode(Agent{ID: "a1", Name: "nanobots"})
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL

	enabled := true
	if _, err := c.UpdateAgent("a1", UpdateAgentRequest{ExecutionIntentsEnabled: &enabled}); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if gotBody["execution_intents_enabled"] != true {
		t.Errorf("request body execution_intents_enabled = %v, want true", gotBody["execution_intents_enabled"])
	}
}

func TestDeleteAgentSendsDELETE(t *testing.T) {
	var gotMethod string
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/v1/auth/api-key-token": tokenHandler(t),
		"/v1/agents/a1": func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			w.WriteHeader(http.StatusNoContent)
		},
	})
	c := NewClient("1ck_test")
	c.BaseURL = srv.URL

	if err := c.DeleteAgent("a1"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
}

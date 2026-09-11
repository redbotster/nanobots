package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// testServerForCompose layers a fake 1Claw (agents + Shroud, in addition to
// vault/secrets) onto testServer, with a real temp AgentStateDir so
// EnsureAgent's local credential caching has somewhere valid to write.
func testServerForCompose(t *testing.T, shroudResponse string) *Server {
	t.Helper()
	srv := testServer(t)
	srv.OneClaw = fakeOneClaw(t)
	srv.Orchestrator.AgentStateDir = t.TempDir()

	shroudMux := http.NewServeMux()
	shroudMux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": shroudResponse}}},
		})
	})
	shroudSrv := httptest.NewServer(shroudMux)
	t.Cleanup(shroudSrv.Close)
	origShroudURL := oneclaw.DefaultShroudURL
	oneclaw.DefaultShroudURL = shroudSrv.URL
	t.Cleanup(func() { oneclaw.DefaultShroudURL = origShroudURL })

	return srv
}

func TestHandleComposeRejectsWhenOneClawNotConfigured(t *testing.T) {
	srv := testServer(t) // no OneClaw set
	body, _ := json.Marshal(composeRequest{Message: "recap my inbox"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleComposeRejectsEmptyMessage(t *testing.T) {
	srv := testServerForCompose(t, "{}")
	body, _ := json.Marshal(composeRequest{Message: "  "})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleComposeProducesAValidatedDraft(t *testing.T) {
	modelResponse := `{
		"name": "Daily inbox recap",
		"description": "Recap my inbox and email me the link",
		"bots": [
			{"id": "recap", "use": "recap-emails-to-pdf@0.3.0"},
			{"id": "mailer", "use": "email-drive-file@1.1.0"}
		],
		"snaps": [
			{"from": "recap.drive_file_id", "to": "mailer.file_id"}
		]
	}`
	srv := testServerForCompose(t, modelResponse)
	body, _ := json.Marshal(composeRequest{Message: "Help me automate a daily email recap and list it by priority"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp composeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Draft.Name != "Daily inbox recap" || len(resp.Draft.Bots) != 2 || len(resp.Draft.Snaps) != 1 {
		t.Fatalf("draft = %+v", resp.Draft)
	}
	if !resp.Plan.OK {
		t.Errorf("expected the composed draft to type-check, got plan=%+v", resp.Plan)
	}
}

func TestHandleComposeSurfacesATypeMismatchRatherThanFailing(t *testing.T) {
	// A hallucinated/bad snap should come back as a normal validation
	// failure in Plan, not a 500 — the human reviews it in the builder.
	modelResponse := `{
		"name": "Bad draft",
		"bots": [
			{"id": "recap", "use": "recap-emails-to-pdf@0.3.0"},
			{"id": "mailer", "use": "email-drive-file@1.1.0"}
		],
		"snaps": [
			{"from": "recap.recap_json", "to": "mailer.file_id"}
		]
	}`
	srv := testServerForCompose(t, modelResponse)
	body, _ := json.Marshal(composeRequest{Message: "anything"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even for a type mismatch, body: %s", rec.Code, rec.Body.String())
	}
	var resp composeResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Plan.OK {
		t.Error("expected the plan to report ok:false for a type-mismatched snap")
	}
}

func TestHandleComposeRejectsUnparsableModelResponse(t *testing.T) {
	srv := testServerForCompose(t, "not json at all")
	body, _ := json.Marshal(composeRequest{Message: "anything"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for an unparsable model response, body: %s", rec.Code, rec.Body.String())
	}
}

func TestParseComposeDraftStripsMarkdownCodeFence(t *testing.T) {
	raw := "```json\n{\"name\": \"X\", \"bots\": [], \"snaps\": []}\n```"
	draft, err := parseComposeDraft(raw)
	if err != nil {
		t.Fatalf("parseComposeDraft: %v", err)
	}
	if draft.Name != "X" {
		t.Errorf("draft = %+v", draft)
	}
}

func TestComposePromptListsCatalogBots(t *testing.T) {
	bots := []BotSummary{
		{ID: "recap-emails-to-pdf", Version: "0.3.0", Name: "recap-emails-to-pdf", Description: "Summarise mail"},
	}
	prompt := composePrompt(bots, "recap my inbox")
	if !strings.Contains(prompt, "recap-emails-to-pdf@0.3.0") || !strings.Contains(prompt, "recap my inbox") {
		t.Errorf("prompt missing expected content:\n%s", prompt)
	}
}

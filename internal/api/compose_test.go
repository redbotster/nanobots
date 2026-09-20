package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/schema"
)

// testServerForCompose layers a fake 1Claw (agents + Shroud, in addition to
// vault/secrets) onto testServer, with a real temp AgentStateDir so
// EnsureAgent's local credential caching has somewhere valid to write.
func testServerForCompose(t *testing.T, shroudResponse string) *Server {
	t.Helper()
	srv := testServer(t)
	srv.OneClaw = fakeOneClaw(t)
	srv.Orchestrator.AgentStateDir = t.TempDir()
	// The default backend: a Shroud marker, resolved by the composer
	// against its own agent. Keeps these tests on the path most
	// deployments actually take.
	srv.Orchestrator.LLM = &llm.DeferredShroud{}

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

func TestHandleComposeRejectsWhenNoModelIsConfigured(t *testing.T) {
	srv := testServer(t) // no OneClaw, no LLM
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
			{"id": "mailer", "use": "email-drive-file@1.1.0", "inputs": {"to": "me@example.com"}}
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
			{"id": "mailer", "use": "email-drive-file@1.1.0", "inputs": {"to": "me@example.com"}}
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

// A model response using the newer per-bot fields must actually survive
// the round trip: decoded into builderBotRef, type-checked by the real
// planner, and handed back in the draft the WebUI shows — not silently
// dropped, which is exactly the bug v3 Phase 8 fixed in the builder's own
// save path (see swarmmerge_test.go).
func TestHandleComposeCanUseTheNewerPerBotFields(t *testing.T) {
	modelResponse := `{
		"name": "Watch with retry",
		"description": "Watch a competitor's pages, retrying on a flaky fetch",
		"bots": [
			{
				"id": "watch", "use": "competitor-watch@0.1.0",
				"inputs": {"urls": ["https://example.com"]},
				"retry": 2, "retry_backoff": "5s", "when": "{{inputs.urls}}"
			}
		],
		"snaps": []
	}`
	srv := testServerForCompose(t, modelResponse)
	body, _ := json.Marshal(composeRequest{Message: "watch a competitor's pages, retrying on a flaky fetch"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp composeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Plan.OK {
		t.Fatalf("expected the draft to type-check, got plan=%+v", resp.Plan)
	}
	if len(resp.Draft.Bots) != 1 {
		t.Fatalf("draft.Bots = %+v", resp.Draft.Bots)
	}
	got := resp.Draft.Bots[0]
	if got.Retry != 2 || got.RetryBackoff != "5s" || got.When != "{{inputs.urls}}" {
		t.Errorf("got = %+v, want retry/retry_backoff/when carried through", got)
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

func TestParseComposeResponseStripsMarkdownCodeFence(t *testing.T) {
	raw := "```json\n{\"name\": \"X\", \"bots\": [], \"snaps\": []}\n```"
	draft, gap, err := parseComposeResponse(raw)
	if err != nil {
		t.Fatalf("parseComposeResponse: %v", err)
	}
	if gap != nil {
		t.Fatalf("expected no gap, got %+v", gap)
	}
	if draft.Name != "X" {
		t.Errorf("draft = %+v", draft)
	}
}

func TestParseComposeResponseParsesAGap(t *testing.T) {
	raw := `{"gap": true, "missing_capability": "post to a personal blog", "suggested_inputs": [{"name": "post", "type": "string"}]}`
	draft, gap, err := parseComposeResponse(raw)
	if err != nil {
		t.Fatalf("parseComposeResponse: %v", err)
	}
	if draft != nil {
		t.Fatalf("expected no draft, got %+v", draft)
	}
	if gap == nil || gap.MissingCapability != "post to a personal blog" {
		t.Fatalf("gap = %+v", gap)
	}
	if len(gap.SuggestedInputs) != 1 || gap.SuggestedInputs[0].Name != "post" {
		t.Errorf("suggested inputs = %+v", gap.SuggestedInputs)
	}
}

func TestParseComposeResponseRejectsGapWithoutMissingCapability(t *testing.T) {
	raw := `{"gap": true}`
	if _, _, err := parseComposeResponse(raw); err == nil {
		t.Fatal("expected an error for a gap with no missing_capability")
	}
}

func TestHandleComposeReturnsGapWhenModelDeclaresOne(t *testing.T) {
	modelResponse := `{"gap": true, "missing_capability": "text my smart lights on and off"}`
	srv := testServerForCompose(t, modelResponse)
	body, _ := json.Marshal(composeRequest{Message: "text my smart lights on and off"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a declared gap, body: %s", rec.Code, rec.Body.String())
	}
	var resp composeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Draft != nil || resp.Plan != nil {
		t.Fatalf("expected no draft/plan on a gap response, got %+v", resp)
	}
	if resp.Gap == nil || resp.Gap.MissingCapability != "text my smart lights on and off" {
		t.Fatalf("gap = %+v", resp.Gap)
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

// fixedGenerator answers with canned text, standing in for any direct
// provider. It is deliberately not a Shroud client and not an httptest
// server: the point of this test is that neither is required.
type fixedGenerator struct {
	answers []string
	prompts []string
}

func (f *fixedGenerator) Describe() string { return "gemini (direct)" }

func (f *fixedGenerator) Generate(_ context.Context, prompt string, _ schema.Model) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if len(f.answers) == 0 {
		return "", fmt.Errorf("no more canned answers")
	}
	out := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return out, nil
}

// The composer is the product's primary entry point, and it used to demand
// 1Claw specifically — not a model, 1Claw. Someone who had added a Gemini
// key and watched their bots run got a 400 from the one box on the page
// that invites them to type something.
func TestComposeWorksWithADirectProviderAndNoOneClawAtAll(t *testing.T) {
	srv := testServer(t)
	srv.OneClaw = nil
	srv.Orchestrator.LLM = &fixedGenerator{answers: []string{`{
		"name": "Daily inbox recap",
		"description": "Recap my inbox and email me the link",
		"bots": [
			{"id": "recap", "use": "recap-emails-to-pdf@0.3.0"},
			{"id": "mailer", "use": "email-drive-file@1.1.0", "inputs": {"to": "me@example.com"}}
		],
		"snaps": [{"from": "recap.drive_file_id", "to": "mailer.file_id"}]
	}`}}

	body, _ := json.Marshal(composeRequest{Message: "recap my inbox"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var out composeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Draft == nil || len(out.Draft.Bots) != 2 {
		t.Fatalf("draft = %+v", out.Draft)
	}
	if out.Plan == nil || !out.Plan.OK {
		t.Errorf("plan = %+v, want a validated draft", out.Plan)
	}
}

// The one-retry correction path is the composer's own quality guard. It has
// to work on every backend, not just the one it was written against.
func TestTheRetryCorrectionAlsoWorksOnADirectProvider(t *testing.T) {
	srv := testServer(t)
	srv.OneClaw = nil
	gen := &fixedGenerator{answers: []string{
		// A json output snapped into a string input — exactly the mistake
		// the correction pass exists for.
		`{"name":"x","description":"d","bots":[
			{"id":"triage","use":"inbox-triage@0.1.0"},{"id":"note","use":"notify@0.1.0"}],
		  "snaps":[{"from":"triage.triaged_count","to":"note.message"}]}`,
		// Corrected on the second call.
		`{"name":"x","description":"d","bots":[
			{"id":"triage","use":"inbox-triage@0.1.0"},{"id":"note","use":"notify@0.1.0"}],
		  "snaps":[]}`,
	}}
	srv.Orchestrator.LLM = gen

	body, _ := json.Marshal(composeRequest{Message: "triage and tell me"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/compose", bytes.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if len(gen.prompts) != 2 {
		t.Fatalf("made %d model calls, want 2 — the correction pass never ran", len(gen.prompts))
	}
	// The retry has to carry the planner's actual complaint, or the model
	// is being asked to fix something it can't see.
	if !strings.Contains(gen.prompts[1], "triaged_count") {
		t.Errorf("the retry prompt does not name the failing snap:\n%s", gen.prompts[1])
	}
}

// The composer's prompt is the whole of what it knows. The language grew
// twice — fan-out with a join, and a per-bot error policy — and the prompt
// did not, so the composer produced swarms in an older dialect: no fan-out
// at all, a hardcoded "chaser.draft_ids[0]" string where a snap belonged,
// and every notification able to fail a run.
func TestTheComposerIsTaughtTheWholeLanguage(t *testing.T) {
	prompt := composePrompt([]BotSummary{{ID: "x", Name: "x", Version: "0.1.0"}}, "do a thing")
	for _, want := range []string{
		".*",       // fan-out
		"join",     // and the way back
		"lines",    // its modes
		"on_error", // the error policy
		"continue", //
		"never",    // a value is not a port reference
		"retry",    // v3 Phase 8: the five newer per-bot fields
		"retry_backoff",
		"when",
		"fallback",
		"loop",
		"until",
		"feed",
	} {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(want)) {
			t.Errorf("the prompt never mentions %q", want)
		}
	}
}

// The composer builds a flat swarm from a single message; it never has an
// existing swarm file to point a "swarm" bot at, so it must not be taught
// to invent one — a nonexistent path there is a worse failure than the
// planner catches, since nothing type-checks a file that was never real.
func TestTheComposerIsNotToldToNestSwarms(t *testing.T) {
	prompt := composePrompt([]BotSummary{{ID: "x", Name: "x", Version: "0.1.0"}}, "do a thing")
	if !strings.Contains(prompt, "Do not compose a bot that references another") {
		t.Error("the prompt does not warn the model off nesting a swarm reference")
	}
}

// A snap can drill into a json port's fields, and the model can only do
// that if it knows the fields exist. Shown types but not fields, it
// invented `.summary` on a port that has none — which fails exactly like a
// misspelled port.
func TestTheCatalogPromptListsAJSONPortsFields(t *testing.T) {
	bots := []BotSummary{{
		ID: "chaser", Name: "invoice-chaser", Version: "0.1.0",
		Outputs:      []schema.OutputPort{{Name: "drafted", Type: "list<json>", Schema: "./schemas/drafted.json"}},
		OutputFields: map[string][]string{"drafted": {"draft_id", "subject", "to"}},
	}}
	prompt := composePrompt(bots, "chase invoices")
	if !strings.Contains(prompt, "drafted:list<json>{draft_id,subject,to}") {
		t.Errorf("fields missing from the catalog line:\n%s", prompt)
	}
}

// The retry pass can only fix what it is shown. Swarm-level problems — an
// unrecognised on_error, a port both snapped and set — were making a draft
// unrunnable while the retry prompt listed nothing wrong with it.
func TestTheRetryPromptCarriesSwarmLevelProblems(t *testing.T) {
	plan := planResponse{
		Invalid: []string{`notifier.message has both a snap and a value in inputs:`},
		Unfed:   []unfedInputJSON{{Bot: "mailer", Port: "to", Reason: "nothing supplies it"}},
	}
	prompt := composeRetryPrompt(nil, "do a thing", &saveSwarmRequest{Name: "x"}, plan)
	for _, want := range []string{"notifier.message", "both a snap and a value", "mailer", "nothing supplies it"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the retry prompt does not mention %q", want)
		}
	}
}

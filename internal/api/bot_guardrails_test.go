package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// Run the editor against every real bot file with a representative set of
// values — the same discipline TestSetInstructionsDefaultAgainstRealBotFiles
// applies, extended to a block with seven heterogeneous fields instead of
// one line. What matters is not "the line count stays the same" (fields can
// be inserted or removed) but that every line *outside* the guardrails
// block survives untouched, and the block, once edited, parses back to
// exactly what was asked for.
func TestSetGuardrailsInYAMLAgainstRealBotFiles(t *testing.T) {
	botsDir := filepath.Join(repoRoot(t), "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatal(err)
	}

	want := schema.Guardrails{
		PII:                 "block",
		InjectionThreshold:  0.9,
		MaxRuntimeSecs:      1800,
		DailyBudgetUSD:      2.5,
		NetworkEgress:       []string{"api.example.com", "*.example.org"},
		WritesAllowed:       []string{"gmail", "slack"},
		ApprovalRequiredFor: []string{"*"},
	}

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(botsDir, e.Name(), "nanobot.yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		checked++

		t.Run(e.Name(), func(t *testing.T) {
			edited, err := setGuardrailsInYAML(raw, want)
			if err != nil {
				t.Fatalf("edit: %v", err)
			}

			tmp := filepath.Join(t.TempDir(), "nanobot.yaml")
			if err := os.WriteFile(tmp, edited, 0o644); err != nil {
				t.Fatal(err)
			}
			reloaded, err := schema.LoadNanobot(tmp)
			if err != nil {
				t.Fatalf("edited bot no longer loads: %v\n%s", err, edited)
			}
			if !guardrailsEqual(reloaded.Spec.Guardrails, want) {
				t.Errorf("guardrails = %+v, want %+v", reloaded.Spec.Guardrails, want)
			}

			// Nothing outside the guardrails block moved: every line before
			// its start and after its (original) end is identical.
			before := strings.Split(string(raw), "\n")
			after := strings.Split(string(edited), "\n")
			blockStart, blockEnd, _, found := guardrailsBlockRange(before)
			if !found {
				t.Fatal("every catalog bot is expected to declare guardrails:")
			}
			for i := 0; i < blockStart; i++ {
				if before[i] != after[i] {
					t.Errorf("line %d before the block changed: %q -> %q", i, before[i], after[i])
				}
			}
			tailBefore := before[blockEnd:]
			tailAfter := after[len(after)-len(tailBefore):]
			for i := range tailBefore {
				if tailBefore[i] != tailAfter[i] {
					t.Errorf("line after the block changed: %q -> %q", tailBefore[i], tailAfter[i])
				}
			}
		})
	}
	if checked < 30 {
		t.Fatalf("only %d bots checked; expected the whole catalog", checked)
	}
	t.Logf("editor verified against %d real bot files", checked)
}

// review-responder's network_egress is a real multi-line flow list — the
// exact shape this editor exists to collapse safely rather than corrupt.
func TestSetGuardrailsInYAMLCollapsesAMultiLineFlowList(t *testing.T) {
	path := filepath.Join(repoRoot(t), "bots", "review-responder", "nanobot.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("network_egress:\n")) {
		t.Fatal("review-responder no longer declares network_egress as a multi-line flow list — pick a different fixture for this test")
	}

	nb, err := schema.LoadNanobot(path)
	if err != nil {
		t.Fatal(err)
	}
	g := nb.Spec.Guardrails
	g.NetworkEgress = []string{"example.com"}

	edited, err := setGuardrailsInYAML(raw, g)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !bytes.Contains(edited, []byte("network_egress: [example.com]")) {
		t.Errorf("expected a collapsed one-line network_egress, got:\n%s", edited)
	}

	tmp := filepath.Join(t.TempDir(), "nanobot.yaml")
	os.WriteFile(tmp, edited, 0o644)
	reloaded, err := schema.LoadNanobot(tmp)
	if err != nil {
		t.Fatalf("edited bot no longer loads: %v\n%s", err, edited)
	}
	if len(reloaded.Spec.Guardrails.NetworkEgress) != 1 || reloaded.Spec.Guardrails.NetworkEgress[0] != "example.com" {
		t.Errorf("network_egress = %v", reloaded.Spec.Guardrails.NetworkEgress)
	}
}

func TestSetGuardrailsInYAMLInsertsAMissingFieldAndRemovesAClearedOne(t *testing.T) {
	path := filepath.Join(repoRoot(t), "bots", "content-ideas", "nanobot.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nb, err := schema.LoadNanobot(path)
	if err != nil {
		t.Fatal(err)
	}
	if nb.Spec.Guardrails.DailyBudgetUSD != 0 {
		t.Fatal("content-ideas already declares a daily budget — pick a different fixture")
	}

	g := nb.Spec.Guardrails
	g.DailyBudgetUSD = 3 // insert a field that wasn't there
	edited, err := setGuardrailsInYAML(raw, g)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(edited, []byte("daily_budget_usd: 3")) {
		t.Errorf("expected daily_budget_usd to be inserted, got:\n%s", edited)
	}

	// Now clear it again — the line must be removed entirely, not left
	// behind as "daily_budget_usd: 0", which would claim a real cap where
	// the bot actually declares none.
	g.DailyBudgetUSD = 0
	cleared, err := setGuardrailsInYAML(edited, g)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleared, []byte("daily_budget_usd")) {
		t.Errorf("expected daily_budget_usd to be removed entirely, got:\n%s", cleared)
	}
}

// Found live, driving this in a real browser: inserting a new field used
// to absorb the blank line this repo's own style leaves between
// guardrails: and the next top-level key into whichever existing field
// happened to sit last, so updating that field's line deleted the blank
// line along with it. inbox-triage is the real fixture that caught it —
// pii/injection_threshold/max_runtime_secs/network_egress/writes_allowed
// declared, no daily_budget_usd, a blank line, then resources:.
func TestSetGuardrailsInYAMLPreservesTheBlankLineBeforeTheNextKey(t *testing.T) {
	path := filepath.Join(repoRoot(t), "bots", "inbox-triage", "nanobot.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nb, err := schema.LoadNanobot(path)
	if err != nil {
		t.Fatal(err)
	}
	if nb.Spec.Guardrails.DailyBudgetUSD != 0 {
		t.Fatal("inbox-triage already declares a daily budget — pick a different fixture")
	}

	g := nb.Spec.Guardrails
	g.DailyBudgetUSD = 2 // insert a field, exercising the field right before the blank line
	edited, err := setGuardrailsInYAML(raw, g)
	if err != nil {
		t.Fatal(err)
	}
	// The blank line must still be there somewhere, not silently deleted —
	// exactly where the new field lands relative to it is a cosmetic
	// question this test does not prescribe.
	wantBlanks := strings.Count(string(raw), "\n\n")
	gotBlanks := strings.Count(string(edited), "\n\n")
	if gotBlanks != wantBlanks {
		t.Errorf("blank line count = %d, want %d (the separator before resources: was lost), got:\n%s",
			gotBlanks, wantBlanks, edited)
	}

	// And putting it back (removing daily_budget_usd again) must restore
	// the file exactly byte for byte, blank line included.
	back, err := setGuardrailsInYAML(edited, nb.Spec.Guardrails)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, raw) {
		t.Errorf("round trip did not restore the original file exactly:\n--- got ---\n%s\n--- want ---\n%s", back, raw)
	}
}

func TestSetGuardrailsInYAMLRejectsABotWithNoGuardrailsBlock(t *testing.T) {
	raw := []byte("apiVersion: nanobots.dev/v1alpha1\nkind: Nanobot\nspec:\n  harness:\n    type: bare\n")
	if _, err := setGuardrailsInYAML(raw, schema.Guardrails{PII: "redact"}); err == nil {
		t.Error("expected an error rather than silently doing nothing")
	}
}

func TestValidateGuardrailsRejectsAnInvalidPII(t *testing.T) {
	nb := &schema.Nanobot{}
	if err := validateGuardrails(nb, schema.Guardrails{PII: "sometimes"}); err == nil {
		t.Error("expected an error for an unrecognized pii value")
	}
}

func TestValidateGuardrailsRejectsAnOutOfRangeInjectionThreshold(t *testing.T) {
	nb := &schema.Nanobot{}
	for _, bad := range []float64{-0.1, 1.1} {
		if err := validateGuardrails(nb, schema.Guardrails{PII: "redact", InjectionThreshold: bad}); err == nil {
			t.Errorf("threshold %v: expected an error", bad)
		}
	}
}

// The real invariant TestABotThatAsksAPersonWaitsLongEnoughForOne
// (internal/contract) exists to enforce, checked here at write time
// instead of only discovered by that test later.
func TestValidateGuardrailsRejectsATooShortRuntimeOnABotThatAsks(t *testing.T) {
	nb, err := schema.LoadNanobot(filepath.Join(repoRoot(t), "bots", "approve", "nanobot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	g := nb.Spec.Guardrails
	g.MaxRuntimeSecs = 60 // far short of the 30-minute approval window
	if err := validateGuardrails(nb, g); err == nil {
		t.Error("expected an error: this would kill the run before the approval window even elapses")
	}

	// The bot's own shipped value (1800s) must still be accepted.
	if err := validateGuardrails(nb, nb.Spec.Guardrails); err != nil {
		t.Errorf("the bot's own shipped guardrails were rejected: %v", err)
	}

	// 0 (undeclared) is exempt — that is the runner's own default's
	// business, not a per-bot claim this validates.
	g.MaxRuntimeSecs = 0
	if err := validateGuardrails(nb, g); err != nil {
		t.Errorf("max_runtime_secs: 0 should be exempt, got: %v", err)
	}
}

func TestValidateGuardrailsRejectsAnApprovalRequiredForNamingANonexistentStep(t *testing.T) {
	nb, err := schema.LoadNanobot(filepath.Join(repoRoot(t), "bots", "invoice-chaser", "nanobot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	g := nb.Spec.Guardrails
	g.ApprovalRequiredFor = []string{"a-step-that-does-not-exist"}
	if err := validateGuardrails(nb, g); err == nil {
		t.Error("expected an error: this would silently require approval for nothing")
	}

	// "*" is always valid, regardless of what steps exist.
	g.ApprovalRequiredFor = []string{"*"}
	if err := validateGuardrails(nb, g); err != nil {
		t.Errorf("\"*\" should always be accepted, got: %v", err)
	}
}

func TestHandleSetBotGuardrails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(repoRoot(t), "bots", "content-ideas")
	dst := filepath.Join(dir, "content-ideas")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(src, "nanobot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "nanobot.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	team := &TeamStore{Path: filepath.Join(dir, "team.json")}
	srv := &Server{BotsDir: dir, Team: team}

	post := func(id, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/bots/"+id+"/guardrails", bytes.NewBufferString(body))
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	// content-ideas ships with exactly {pii: allow, max_runtime_secs: 60}.
	rec := post("content-ideas", `{"pii":"block","injection_threshold":0.5,"max_runtime_secs":60,"daily_budget_usd":1.5,"network_egress":["a.com"],"writes_allowed":[],"approval_required_for":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var summary BotSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Guardrails.PII != "block" || summary.Guardrails.DailyBudgetUSD != 1.5 {
		t.Errorf("returned summary = %+v", summary.Guardrails)
	}

	// It's now recorded as tuned, with the real shipped value kept.
	shipped, known := team.ShippedGuardrails("content-ideas")
	if !known || shipped.PII != "allow" || shipped.MaxRuntimeSecs != 60 {
		t.Errorf("ShippedGuardrails = %+v, known=%v, want {pii: allow, max_runtime_secs: 60}", shipped, known)
	}

	// An invalid request is rejected before anything is written.
	rec = post("content-ideas", `{"pii":"whenever"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	nb, _ := schema.LoadNanobot(filepath.Join(dst, "nanobot.yaml"))
	if nb.Spec.Guardrails.PII != "block" {
		t.Errorf("a rejected request must not partially write: pii = %q", nb.Spec.Guardrails.PII)
	}

	// Putting it back to exactly what shipped clears the tuned record.
	rec = post("content-ideas", `{"pii":"allow","max_runtime_secs":60}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if _, known := team.ShippedGuardrails("content-ideas"); known {
		t.Error("expected the guardrails tuned record to be cleared after putting it back")
	}

	// A missing bot 404s rather than writing anywhere.
	rec = post("nope", `{"pii":"redact"}`)
	if rec.Code == http.StatusOK {
		t.Error("a missing bot should not succeed")
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/step"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file: internal/api/server_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func testServer(t *testing.T) *Server {
	t.Helper()
	root := repoRoot(t)
	blobs, err := step.NewFSBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		Orchestrator: &runner.Orchestrator{RepoRoot: root, BotsDir: filepath.Join(root, "bots")},
		Runs:         runner.NewRunStore(),
		Callbacks:    runner.NewCallbackRegistry(),
		BotsDir:      filepath.Join(root, "bots"),
		Blobs:        blobs,
	}
}

func TestHandleStatus(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); got == "" {
		t.Fatal("expected a JSON body")
	}
}

func TestHandleListBots(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/bots", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"recap-emails-to-pdf", "email-drive-file"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("expected bot list to mention %q, got: %s", want, rec.Body.String())
		}
	}
}

// TestHandleListBotsNeverReturnsNullArrays catches a real crash: a bot with
// no services: block (e.g. bots/notify) has a nil Services slice in Go,
// which encoding/json renders as `null` — and a frontend that assumes an
// array (bot.services.map(...)) crashes outright. Every bot's
// services/inputs/outputs/tags must serialize as [], never null.
func TestHandleListBotsNeverReturnsNullArrays(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/bots", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var bots []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &bots); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, b := range bots {
		if b["id"] != "notify" {
			continue
		}
		found = true
		for _, field := range []string{"services", "inputs", "outputs", "tags"} {
			if b[field] == nil {
				t.Errorf("bot %q field %q is null, want an empty array", b["id"], field)
			}
		}
	}
	if !found {
		t.Fatal("expected to find the notify bot (it has no services: block) in the list")
	}
}

func TestHandleListSwarms(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/swarms", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"daily-email-recap", "daily-inbox-recap", "morning-brief", "inbox-autopilot"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("expected swarm list to mention %q, got: %s", want, rec.Body.String())
		}
	}
}

func TestHandleListSwarmsReportsServiceLiveness(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/swarms", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var swarms []SwarmSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &swarms); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found *SwarmSummary
	for i := range swarms {
		if swarms[i].Name == "daily-email-recap" || strings.Contains(swarms[i].Path, "daily-email-recap") {
			found = &swarms[i]
		}
	}
	if found == nil {
		t.Fatal("daily-email-recap not found in listing")
	}
	if found.ServicesTotal == 0 {
		t.Error("expected daily-email-recap to declare at least one service")
	}
	if found.ServicesLive != 0 {
		t.Errorf("ServicesLive = %d, want 0 — every shipped bot defaults to connection: demo", found.ServicesLive)
	}
	if found.LastRunID != "" {
		t.Errorf("expected no last run for a swarm that's never executed, got %+v", found)
	}
}

func TestHandleListSwarmsReportsLastRun(t *testing.T) {
	srv := testServer(t)
	run := runner.NewRun("daily-email-recap")
	run.TriggeredBy = "schedule"
	run.SetStatus(runner.StatusSucceeded)
	srv.Runs.Add(run)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/swarms", nil))
	var swarms []SwarmSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &swarms); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found *SwarmSummary
	for i := range swarms {
		if swarms[i].Name == "daily-email-recap" {
			found = &swarms[i]
		}
	}
	if found == nil {
		t.Fatal("daily-email-recap not found in listing")
	}
	if found.LastRunID != run.ID {
		t.Errorf("LastRunID = %q, want %q", found.LastRunID, run.ID)
	}
	if found.LastRunStatus != "succeeded" {
		t.Errorf("LastRunStatus = %q, want succeeded", found.LastRunStatus)
	}
	if found.LastRunTrigger != "schedule" {
		t.Errorf("LastRunTrigger = %q, want schedule", found.LastRunTrigger)
	}
	if found.LastRunAt == "" {
		t.Error("expected LastRunAt to be set")
	}
}

func TestLastRunForPicksTheMostRecentMatchingRun(t *testing.T) {
	older := runner.NewRun("x")
	older.StartedAt = time.Now().Add(-time.Hour)
	newer := runner.NewRun("x")
	unrelated := runner.NewRun("y")

	got := lastRunFor([]*runner.Run{older, newer, unrelated}, "x")
	if got == nil || got.ID != newer.ID {
		t.Errorf("lastRunFor = %v, want the newer run", got)
	}
}

func TestLastRunForReturnsNilWhenNoneMatch(t *testing.T) {
	if got := lastRunFor([]*runner.Run{runner.NewRun("x")}, "y"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestHandlePlan(t *testing.T) {
	srv := testServer(t)
	root := repoRoot(t)
	path := filepath.Join(root, "examples", "swarms", "daily-email-recap.yaml")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/swarms/plan?path="+path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("expected the plan to type-check, got: %s", rec.Body.String())
	}
}

func TestHandlePlanMissingPath(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/swarms/plan", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCallbackRejectsMissingToken(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/steps/memory_get", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a request with no run token", rec.Code)
	}
}

func TestCallbackRejectsUnknownToken(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/internal/steps/memory_get", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an unregistered run token", rec.Code)
	}
}

func TestSwarmYAMLRejectsPathTraversal(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/swarms/yaml?path=../../etc/passwd", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a path containing ..", rec.Code)
	}
}

// The probe shells out to `docker version`, so the cache is what keeps a
// polled status endpoint from doing that on every request from every tab.
func TestDockerProbeCachesWithinTTL(t *testing.T) {
	var p dockerProbe
	start := time.Now()

	// First call populates the cache by really probing; whatever it finds
	// (this test must pass on a machine with and without Docker running) is
	// what every call inside the TTL has to keep returning.
	wantOK, wantReason := p.get(start)
	firstCheckedAt := p.checkedAt

	if gotOK, gotReason := p.get(start.Add(dockerProbeTTL - time.Millisecond)); gotOK != wantOK || gotReason != wantReason {
		t.Errorf("inside TTL = (%v, %q), want the cached (%v, %q)", gotOK, gotReason, wantOK, wantReason)
	}
	if !p.checkedAt.Equal(firstCheckedAt) {
		t.Error("a call inside the TTL re-probed; the cache did nothing")
	}

	p.get(start.Add(dockerProbeTTL + time.Millisecond))
	if p.checkedAt.Equal(firstCheckedAt) {
		t.Error("a call past the TTL used the stale cache; Docker coming back would never be noticed")
	}
}

func TestStatusReportsDockerSeparatelyFromOneClaw(t *testing.T) {
	// No OneClaw client configured, so oneclaw_configured must be false
	// regardless of what the machine's Docker is doing — the two are
	// independent signals and the UI treats them as such.
	srv := &Server{BotsDir: t.TempDir()}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["oneclaw_configured"] != false {
		t.Errorf("oneclaw_configured = %v, want false", got["oneclaw_configured"])
	}
	available, ok := got["docker_available"].(bool)
	if !ok {
		t.Fatalf("docker_available = %#v, want a bool", got["docker_available"])
	}
	reason, _ := got["docker_reason"].(string)
	// The machine running this may or may not have Docker up; what must
	// always hold is that an unavailable Docker comes with something to
	// show the user, and an available one doesn't nag.
	if available && reason != "" {
		t.Errorf("docker_available with reason %q, want no reason", reason)
	}
	if !available && reason == "" {
		t.Error("docker unavailable with an empty reason; the banner would render blank")
	}
}

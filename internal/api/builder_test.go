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
	"time"

	"github.com/redbotster/nanobots/internal/schema"
)

// testServerWithTempSwarms is testServer plus a scratch SwarmsDir, so
// handleSaveSwarm's writes never touch this repo's real examples/swarms/.
func testServerWithTempSwarms(t *testing.T) *Server {
	t.Helper()
	srv := testServer(t)
	srv.SwarmsDir = t.TempDir()
	return srv
}

func postJSON(t *testing.T, srv *Server, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHandleValidateSwarmGoodSnapTypeChecks(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms/validate", validateSwarmRequest{
		Bots: []builderBotRef{
			{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"},
			{ID: "mailer", Use: "email-drive-file@1.1.0", Inputs: map[string]any{"to": "me@example.com"}},
		},
		Snaps: []builderSnap{
			{From: "recap.drive_file_id", To: "mailer.file_id"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp planResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected ok:true, got %+v", resp)
	}
	if len(resp.Snaps) != 1 || !resp.Snaps[0].OK {
		t.Errorf("snaps = %+v", resp.Snaps)
	}
}

func TestHandleValidateSwarmTypeMismatchReportsPerSnapErrorNot500(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms/validate", validateSwarmRequest{
		Bots: []builderBotRef{
			{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"},
			{ID: "mailer", Use: "email-drive-file@1.1.0", Inputs: map[string]any{"to": "me@example.com"}},
		},
		// recap_json is json, not a valid string port — a real mismatch.
		Snaps: []builderSnap{
			{From: "recap.recap_json", To: "mailer.file_id"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even for a type mismatch, body: %s", rec.Code, rec.Body.String())
	}
	var resp planResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OK {
		t.Fatal("expected ok:false for a type mismatch")
	}
	if len(resp.Snaps) != 1 || resp.Snaps[0].OK || resp.Snaps[0].Error == "" {
		t.Errorf("snaps = %+v, want one failing snap with an error message", resp.Snaps)
	}
}

func TestHandleValidateSwarmUnknownBotReportsErrorField(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms/validate", validateSwarmRequest{
		Bots: []builderBotRef{{ID: "ghost", Use: "does-not-exist@1.0.0"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var resp planResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OK || resp.Error == "" {
		t.Errorf("resp = %+v, want ok:false with a resolve error", resp)
	}
}

func TestHandleSaveSwarmWritesLoadableYAML(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Name:        "My Test Swarm!",
		Description: "built by the visual builder",
		Bots: []builderBotRef{
			{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"},
			{ID: "mailer", Use: "email-drive-file@1.1.0", Inputs: map[string]any{"to": "me@example.com"}},
		},
		Snaps: []builderSnap{
			{From: "recap.drive_file_id", To: "mailer.file_id"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp saveSwarmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Path == "" {
		t.Fatal("expected a non-empty path")
	}
	if !resp.Plan.OK {
		t.Errorf("expected the saved swarm to type-check, got plan=%+v", resp.Plan)
	}

	// Slugified from the name, and safe to use as a filename.
	wantSlug := "my-test-swarm.yaml"
	if filepath.Base(resp.Path) != wantSlug {
		t.Errorf("path = %q, want basename %q", resp.Path, wantSlug)
	}

	abs := filepath.Join(srv.SwarmsDir, wantSlug)
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("expected a file at %s: %v", abs, err)
	}
	sw, err := schema.LoadNanoswarm(abs)
	if err != nil {
		t.Fatalf("saved file didn't parse back as a Nanoswarm: %v", err)
	}
	if sw.Metadata.Name != "My Test Swarm!" || len(sw.Spec.Bots) != 2 || len(sw.Spec.Snaps) != 1 {
		t.Errorf("loaded swarm = %+v", sw)
	}
}

func TestHandleSaveSwarmRejectsMissingName(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Bots: []builderBotRef{{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSaveSwarmRejectsEmptyBots(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{Name: "Empty"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSaveSwarmRejectsStructurallyBrokenDraft(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Name: "Broken",
		Bots: []builderBotRef{{ID: "ghost", Use: "does-not-exist@1.0.0"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unresolvable bot ref, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSaveSwarmAllowsATypeMismatchAsAWorkInProgress(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Name: "WIP",
		Bots: []builderBotRef{
			{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"},
			{ID: "mailer", Use: "email-drive-file@1.1.0", Inputs: map[string]any{"to": "me@example.com"}},
		},
		Snaps: []builderSnap{{From: "recap.recap_json", To: "mailer.file_id"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a type mismatch shouldn't block saving a draft, body: %s", rec.Code, rec.Body.String())
	}
	var resp saveSwarmResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Plan.OK {
		t.Error("expected the response's plan to report ok:false so the UI can flag it")
	}
}

func TestHandleSaveSwarmTwiceWithSameNameGetsDistinctFiles(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	req := saveSwarmRequest{Name: "Dup", Bots: []builderBotRef{{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"}}}

	var first, second saveSwarmResponse
	json.Unmarshal(postJSON(t, srv, "/api/swarms", req).Body.Bytes(), &first)
	json.Unmarshal(postJSON(t, srv, "/api/swarms", req).Body.Bytes(), &second)

	if first.Path == second.Path {
		t.Errorf("expected distinct paths, both got %q", first.Path)
	}
}

func TestHandleSaveSwarmWithPathOverwritesExistingFile(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	var created saveSwarmResponse
	json.Unmarshal(postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Name: "Editable", Bots: []builderBotRef{{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"}},
	}).Body.Bytes(), &created)

	rec := postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Path: created.Path, Name: "Editable", Description: "now with a description",
		Bots: []builderBotRef{{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var updated saveSwarmResponse
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Path != created.Path {
		t.Errorf("path changed on an edit-save: %q -> %q", created.Path, updated.Path)
	}

	entries, err := os.ReadDir(srv.SwarmsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly one file after an edit-save, got %d", len(entries))
	}
}

func TestHandleGetSwarmFullRoundTripsWhatWasSaved(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	var created saveSwarmResponse
	json.Unmarshal(postJSON(t, srv, "/api/swarms", saveSwarmRequest{
		Name: "Round Trip", Description: "d",
		Bots:  []builderBotRef{{ID: "recap", Use: "recap-emails-to-pdf@0.3.0"}, {ID: "mailer", Use: "email-drive-file@1.1.0", Inputs: map[string]any{"to": "me@example.com"}}},
		Snaps: []builderSnap{{From: "recap.drive_file_id", To: "mailer.file_id"}},
	}).Body.Bytes(), &created)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/swarms/full?path="+created.Path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var full swarmFullResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if full.Name != "Round Trip" || len(full.Bots) != 2 || len(full.Snaps) != 1 {
		t.Errorf("full = %+v", full)
	}
}

func TestHandleGetSwarmFullRejectsPathTraversal(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/swarms/full?path=../../etc/passwd", nil))
	if rec.Code == http.StatusOK {
		t.Fatal("expected a non-200 for a path-traversal attempt")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Test Swarm!": "my-test-swarm",
		"  spaces  ":     "spaces",
		"already-a-slug": "already-a-slug",
		"":               "swarm",
		"!!!":            "swarm",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// The builder round-trips a whole swarm on every save, so a field it does
// not know about is a field it deletes. That is not hypothetical: "Save
// changes" once removed a swarm's cron trigger and its guardrails, which
// is why mergeIntoExistingSwarm exists.
//
// `join:` is the newest such field, and losing it would silently turn
// get-paid back into chasing one overdue invoice out of many — no warning,
// no undo, and a swarm that still type-checks afterwards.
func TestASnapsJoinSurvivesTheBuilderRoundTrip(t *testing.T) {
	snap := builderSnap{From: "sender.acted_on", To: "notifier.message", Join: "lines"}

	if got := snap.toSchema().Join; got != "lines" {
		t.Errorf("toSchema dropped the join: %q", got)
	}

	// And back out again, the way the builder loads a swarm for editing.
	sw := draftToNanoswarm("probe", "", "", nil, []builderSnap{snap})
	if len(sw.Spec.Snaps) != 1 || sw.Spec.Snaps[0].Join != "lines" {
		t.Fatalf("join lost building the swarm: %+v", sw.Spec.Snaps)
	}

	// The merge path is the one that actually writes the file.
	existing := []byte(`apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: probe
  description: probe
spec:
  trigger:
    type: cron
    expr: "0 8 * * 1"
  bots:
    - id: notifier
      use: notify@0.1.0
  snaps: []
`)
	merged, err := mergeIntoExistingSwarm(existing, "probe", "probe",
		[]builderBotRef{{ID: "notifier", Use: "notify@0.1.0"}}, []builderSnap{snap}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "join: lines") {
		t.Errorf("the merged YAML has no join:\n%s", merged)
	}
	// The reason merge exists at all — the trigger must still be there.
	if !strings.Contains(string(merged), "0 8 * * 1") {
		t.Errorf("the merge dropped the swarm's trigger:\n%s", merged)
	}
}

// The builder loads a swarm, the human edits one thing, the builder saves
// the whole swarm back. Anything it fails to load, it deletes — which is
// how "Save changes" once removed a cron trigger, and would have removed a
// join or an on_error.
//
// This is the round trip end to end: a real swarm file in, through the
// endpoint the builder loads from, back out through the endpoint it saves
// to, and read again.
func TestABuilderRoundTripPreservesJoinAndOnError(t *testing.T) {
	srv := testServer(t)
	dir := t.TempDir()
	srv.SwarmsDir = dir
	path := filepath.Join(dir, "probe.yaml")
	original := `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: probe
  description: probe
spec:
  trigger:
    type: cron
    expr: "0 8 * * 1"
  bots:
    - id: recap
      use: recap-emails-to-pdf@0.3.0
    - id: mailer
      use: email-drive-file@1.1.0
      inputs:
        to: me@example.com
      on_error: continue
  snaps:
    - from: recap.drive_file_id
      to: mailer.file_id
      join: first
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	// Load it the way the builder does.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/swarms/full?path="+filepath.Base(path), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("load: %d %s", rec.Code, rec.Body.String())
	}
	var loaded swarmFullResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Snaps[0].Join != "first" {
		t.Errorf("join lost on load: %+v", loaded.Snaps[0])
	}
	var mailer builderBotRef
	for _, b := range loaded.Bots {
		if b.ID == "mailer" {
			mailer = b
		}
	}
	if mailer.OnError != "continue" {
		t.Errorf("on_error lost on load: %+v", mailer)
	}

	// Save it straight back, changing only the description — the shape of
	// a human opening a swarm, touching one field, and pressing save.
	body, _ := json.Marshal(saveSwarmRequest{
		Path: path, Name: "probe", Description: "edited",
		Bots: loaded.Bots, Snaps: loaded.Snaps,
	})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/swarms", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"join: first", "on_error: continue", "0 8 * * 1"} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("a builder save dropped %q:\n%s", want, saved)
		}
	}
}

// "Run it at 7am" means 7am where the user is.
//
// The builder shows a swarm's timezone but has never had a control to set
// one, so every swarm saved from the WebUI arrived with "" — which the
// scheduler reads as UTC. On a UTC-7 machine that turned "Weekdays at
// 7:00 AM" into a midnight run while the card went on saying 7:00 AM.
//
// Checked against time.Now()'s own offset rather than against
// localTimezoneName(), which would just be asking the code under test to
// agree with itself. The first version of this test did exactly that and
// passed on a machine where the function returned "" and fixed nothing.
func TestASavedScheduleGetsThisMachinesTimezone(t *testing.T) {
	expr := "0 7 * * 1-5"
	trig, err := triggerFor(&expr, "")
	if err != nil {
		t.Fatal(err)
	}
	if trig.Type != "cron" {
		t.Fatalf("type = %q", trig.Type)
	}
	if trig.Timezone == "" {
		// localTimezoneName returning "" is a documented, honest fallback:
		// a host where TZ is unset, time.Local has no name, and
		// /etc/localtime is a copy rather than a symlink genuinely cannot
		// be resolved, and an unset timezone beats a guessed one. Skipping
		// rather than failing so CI on such a host reports the truth
		// instead of a red build — the case this test is really about,
		// a machine that does know its zone, is checked below.
		t.Skip("this host cannot resolve its own IANA zone; nothing to assert")
	}

	loc, err := time.LoadLocation(trig.Timezone)
	if err != nil {
		t.Fatalf("saved an unloadable timezone %q: %v", trig.Timezone, err)
	}
	// The independent check: whatever zone was chosen must agree with this
	// machine's actual UTC offset right now.
	now := time.Now()
	_, want := now.Zone()
	_, got := now.In(loc).Zone()
	if got != want {
		t.Errorf("saved timezone %q is %+d seconds off UTC, but this machine is %+d",
			trig.Timezone, got, want)
	}
}

// An explicit timezone still wins — this defaults, it does not override.
func TestAnExplicitTimezoneIsKept(t *testing.T) {
	expr := "0 9 * * 1"
	trig, err := triggerFor(&expr, "Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if trig.Timezone != "Asia/Tokyo" {
		t.Errorf("timezone = %q, want the one that was asked for", trig.Timezone)
	}
}

// A manual swarm has no schedule to place in a timezone, and must not
// acquire a cron trigger just because this defaulting exists.
func TestAManualSwarmStaysManual(t *testing.T) {
	for _, sched := range []*string{nil, ptrTo(""), ptrTo("   ")} {
		trig, err := triggerFor(sched, "")
		if err != nil {
			t.Fatal(err)
		}
		if trig.Type != "manual" {
			t.Errorf("type = %q, want manual", trig.Type)
		}
		if trig.Timezone != "" {
			t.Errorf("a manual trigger picked up timezone %q", trig.Timezone)
		}
	}
}

func ptrTo(s string) *string { return &s }

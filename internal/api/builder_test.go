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
		[]builderBotRef{{ID: "notifier", Use: "notify@0.1.0"}}, []builderSnap{snap})
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

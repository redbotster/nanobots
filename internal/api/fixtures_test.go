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

	"github.com/redbotster/nanobots/internal/runner"
)

// botsDirWithFixtures makes a throwaway catalog so a write test never
// touches this repo's real bots/.
func botsDirWithFixtures(t *testing.T, botID string, existing map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, botID)
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte("metadata:\n  name: "+botID+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range existing {
		if err := os.WriteFile(filepath.Join(dir, "fixtures", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runWithCapture(t *testing.T, srv *Server, captured map[string]map[string]any) *runner.Run {
	t.Helper()
	run := runner.NewRun("probe")
	for bot, fx := range captured {
		run.SetCaptured(bot, fx)
	}
	srv.Runs.Add(run)
	return run
}

func getFixtures(t *testing.T, srv *Server, runID string) fixturesResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/fixtures", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out fixturesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The preview has to say what would be *replaced*, not just what would be
// written. Overwriting committed test data is the part worth looking at
// twice, and a button that did it silently would be the worst way to find
// out a fixture changed.
func TestThePreviewDistinguishesNewFromReplaced(t *testing.T) {
	srv := testServer(t)
	srv.BotsDir = botsDirWithFixtures(t, "probe", map[string]string{
		"ai.generate.json": "{\n  \"old\": true\n}\n",
	})
	run := runWithCapture(t, srv, map[string]map[string]any{
		"probe": {
			"ai.generate.json":         map[string]any{"fresh": true},
			"gmail.drafts.create.json": map[string]any{"draft_ids": []any{"d1"}},
		},
	})

	got := getFixtures(t, srv, run.ID)
	byFile := map[string]fixturePreview{}
	for _, p := range got.Bots["probe"] {
		byFile[p.File] = p
	}
	if byFile["ai.generate.json"].Status != "replaces" {
		t.Errorf("existing fixture reported as %q", byFile["ai.generate.json"].Status)
	}
	if !strings.Contains(byFile["ai.generate.json"].Current, "old") {
		t.Error("the preview does not show what is on disk today")
	}
	if byFile["gmail.drafts.create.json"].Status != "new" {
		t.Errorf("new fixture reported as %q", byFile["gmail.drafts.create.json"].Status)
	}
}

// A capture identical to what's committed is not a change, and listing it
// would make every run look like it wants something.
func TestAnUnchangedFixtureIsNotOffered(t *testing.T) {
	srv := testServer(t)
	srv.BotsDir = botsDirWithFixtures(t, "probe", map[string]string{
		"ai.generate.json": "{\n  \"same\": true\n}\n",
	})
	run := runWithCapture(t, srv, map[string]map[string]any{
		"probe": {"ai.generate.json": map[string]any{"same": true}},
	})
	if got := getFixtures(t, srv, run.ID); len(got.Bots) != 0 {
		t.Errorf("offered an unchanged fixture: %+v", got.Bots)
	}
}

// Writing is a separate, deliberate step, and the result names every file
// so it can be checked against a diff.
func TestPinningWritesTheFilesAndSaysWhich(t *testing.T) {
	srv := testServer(t)
	srv.BotsDir = botsDirWithFixtures(t, "probe", nil)
	run := runWithCapture(t, srv, map[string]map[string]any{
		"probe": {"ai.generate.json": map[string]any{"drafts": []any{}}},
	})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/api/runs/"+run.ID+"/fixtures", bytes.NewReader([]byte(`{"bot":"probe"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out pinFixturesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Written) != 1 || out.Written[0] != "probe/ai.generate.json" {
		t.Fatalf("written = %v", out.Written)
	}

	raw, err := os.ReadFile(filepath.Join(srv.BotsDir, "probe", "fixtures", "ai.generate.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Two-space indented and newline-terminated: a pinned fixture has to
	// produce a diff someone can read, not one line of minified JSON.
	if !strings.HasPrefix(string(raw), "{\n  \"drafts\"") || !strings.HasSuffix(string(raw), "}\n") {
		t.Errorf("not written in the shape a person would write:\n%s", raw)
	}
}

// Keeping the model's answer while rejecting a service response that
// happened to be empty today is the normal case, not an edge one.
func TestPinningCanTakeSomeFilesAndNotOthers(t *testing.T) {
	srv := testServer(t)
	srv.BotsDir = botsDirWithFixtures(t, "probe", nil)
	run := runWithCapture(t, srv, map[string]map[string]any{
		"probe": {
			"ai.generate.json": map[string]any{"a": 1},
			"gmail.list.json":  map[string]any{"messages": []any{}},
		},
	})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/api/runs/"+run.ID+"/fixtures",
		bytes.NewReader([]byte(`{"bot":"probe","files":["ai.generate.json"]}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(srv.BotsDir, "probe", "fixtures", "gmail.list.json")); err == nil {
		t.Error("wrote a file that wasn't asked for")
	}
}

// This endpoint writes files into the repo. The bot id comes from a run
// rather than a request, but trusting a path all the way to os.WriteFile is
// how directory traversal happens.
func TestNothingIsWrittenOutsideTheBotsDirectory(t *testing.T) {
	srv := testServer(t)
	srv.BotsDir = botsDirWithFixtures(t, "probe", nil)

	for _, bad := range []string{"../escape", "..", "a/b", `a\b`, ""} {
		if _, err := srv.fixturesDirFor(bad); err == nil {
			t.Errorf("accepted bot id %q", bad)
		}
	}
	// And an unknown bot, which would otherwise create a directory for a
	// bot that doesn't exist.
	if _, err := srv.fixturesDirFor("not-a-bot"); err == nil {
		t.Error("accepted a bot that isn't in the catalog")
	}
}

func TestOnlyFixtureShapedFilenamesAreWritten(t *testing.T) {
	for _, bad := range []string{"../x.json", "a/b.json", "notjson", ".json", `a\b.json`} {
		if safeFixtureName(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, good := range []string{"ai.generate.json", "gmail.messages.list.json", "memory.recall.json"} {
		if !safeFixtureName(good) {
			t.Errorf("rejected %q", good)
		}
	}
}

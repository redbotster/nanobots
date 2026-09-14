package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/foundry"
)

// fakeFoundryAgent is a test-only foundry.Agent — see internal/foundry's
// own fakeAgent for the pattern this mirrors; duplicated here (rather than
// exported from internal/foundry) since it's only ever needed by this
// package's HTTP-layer tests.
type fakeFoundryAgent struct {
	copyFrom string
	newID    string
}

func (f *fakeFoundryAgent) Run(ctx context.Context, workDir string, in foundry.BriefInput, events chan<- foundry.Event) error {
	return copyDirForTest(f.copyFrom, filepath.Join(workDir, "bots", f.newID))
}

func copyDirForTest(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, in, 0o644)
	})
}

func testServerWithFoundry(t *testing.T, agent foundry.Agent) *Server {
	t.Helper()
	srv := testServerWithOneClaw(t)
	botsDir := t.TempDir()
	srv.BotsDir = botsDir
	srv.Foundry = &foundry.Orchestrator{Config: foundry.Config{
		// A throwaway repo, not this one. RepoRoot used to be repoRoot(t),
		// so every run of this test did `git worktree add -b foundry/<uuid>`
		// against the developer's actual checkout and left the branch behind
		// — removeWorktree only cleans up after a successful promote, which
		// is right for a real job and wrong for a test. Found one still
		// registered and prunable weeks later.
		RepoRoot: throwawayRepo(t), BotsDir: botsDir, WorkDir: t.TempDir(),
		Agent: agent,
	}}
	srv.FoundryJobs = foundry.NewJobStore()
	return srv
}

// throwawayRepo makes a git repo with one commit in a temp dir, which is
// all `git worktree add` needs. Mirrors internal/foundry's own
// newTestRepo; this package cannot import that one because it lives in a
// _test.go file over there.
func throwawayRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bots", ".keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("-c", "user.name=test", "-c", "user.email=test@test.local", "add", "-A")
	git("-c", "user.name=test", "-c", "user.email=test@test.local", "commit", "-q", "-m", "init")
	return root
}

func TestHandleStartFoundryJobRejectsWhenFoundryNotConfigured(t *testing.T) {
	srv := testServerWithOneClaw(t) // no Foundry set
	body, _ := json.Marshal(startFoundryJobRequest{MissingCapability: "x"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/foundry", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleStartFoundryJobRejectsWhenOneClawNotConfigured(t *testing.T) {
	srv := testServer(t) // no OneClaw
	srv.Foundry = &foundry.Orchestrator{Config: foundry.Config{Agent: &fakeFoundryAgent{}}}
	srv.FoundryJobs = foundry.NewJobStore()
	body, _ := json.Marshal(startFoundryJobRequest{MissingCapability: "x"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/foundry", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleStartFoundryJobRejectsEmptyMissingCapability(t *testing.T) {
	srv := testServerWithFoundry(t, &fakeFoundryAgent{})
	body, _ := json.Marshal(startFoundryJobRequest{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/foundry", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleStartFoundryJobStartsAJob(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "bots", "content-ideas")
	srv := testServerWithFoundry(t, &fakeFoundryAgent{copyFrom: fixture, newID: "new-content-bot"})

	body, _ := json.Marshal(startFoundryJobRequest{Request: "ideas please", MissingCapability: "brainstorm ideas"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/foundry", bytes.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body: %s", rec.Code, rec.Body.String())
	}
	var started map[string]any
	json.Unmarshal(rec.Body.Bytes(), &started)
	id, _ := started["id"].(string)
	if id == "" {
		t.Fatalf("expected an id in the response, got %+v", started)
	}

	// Poll GET until the review approval registers.
	var pending []any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foundry/"+id, nil))
		var got map[string]any
		json.Unmarshal(rec.Body.Bytes(), &got)
		if pa, ok := got["pending_approvals"].([]any); ok && len(pa) > 0 {
			pending = pa
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(pending))
	}
	approvalID, _ := pending[0].(map[string]any)["id"].(string)
	if approvalID == "" {
		t.Fatalf("pending approval had no id: %+v", pending[0])
	}

	decideBody, _ := json.Marshal(decideApprovalRequest{Approved: true, DecidedBy: "test"})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/foundry/"+id+"/approvals/"+approvalID+"/decide", bytes.NewReader(decideBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("decide status = %d, body: %s", rec.Code, rec.Body.String())
	}

	deadline = time.Now().Add(5 * time.Second)
	var final map[string]any
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foundry/"+id, nil))
		json.Unmarshal(rec.Body.Bytes(), &final)
		if final["outcome"] == "promoted" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final["outcome"] != "promoted" {
		t.Fatalf("outcome = %v, want promoted; full response: %+v", final["outcome"], final)
	}
	if _, err := os.Stat(filepath.Join(srv.BotsDir, "new-content-bot", "nanobot.yaml")); err != nil {
		t.Errorf("expected the bot to be promoted into BotsDir: %v", err)
	}
}

func TestHandleListFoundryJobsEmptyWhenNotConfigured(t *testing.T) {
	srv := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foundry", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var jobs []any
	json.Unmarshal(rec.Body.Bytes(), &jobs)
	if len(jobs) != 0 {
		t.Errorf("jobs = %+v, want empty", jobs)
	}
}

func TestHandleGetFoundryJobNotFound(t *testing.T) {
	srv := testServerWithFoundry(t, &fakeFoundryAgent{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foundry/nonexistent", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

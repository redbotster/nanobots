package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Settings tells you what a key/value memory backend is costing this
// install by naming the bots that ask memory a question. That list used to
// be a sentence in the React component with one bot's name in it, which was
// wrong the moment a second bot gained a recall step. Reading it from the
// catalog is only an improvement if it really tracks the catalog, so this
// runs against the real bots/ directory rather than a fixture.
func TestStatusNamesTheRealBotsThatUseRecall(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	botsDir := filepath.Join(filepath.Dir(file), "..", "..", "bots")

	srv := &Server{BotsDir: botsDir}
	rec := httptest.NewRecorder()
	srv.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Bots []string `json:"memory_recall_bots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	want := botsWithRecallStep(t, botsDir)
	if !reflect.DeepEqual(body.Bots, want) {
		t.Errorf("memory_recall_bots = %v, want %v", body.Bots, want)
	}
	if len(want) == 0 {
		t.Error("no bot in the catalog uses memory.recall — this test is checking nothing")
	}
}

// botsWithRecallStep greps rather than parsing, deliberately: an
// independent second opinion on what botsUsingRecall computes, so the test
// can't agree with the implementation by sharing its bug.
func botsWithRecallStep(t *testing.T, botsDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			continue
		}
		if hasLine(string(raw), "type: memory.recall") {
			ids = append(ids, e.Name())
		}
	}
	return ids
}

func hasLine(haystack, needle string) bool {
	for _, l := range strings.Split(haystack, "\n") {
		if strings.TrimSpace(l) == needle {
			return true
		}
	}
	return false
}

// An empty catalog directory must not crash the status endpoint — it is
// polled by every open tab, and a misconfigured BotsDir is a startup
// problem, not a reason for the whole status panel to go blank.
func TestStatusSurvivesAnUnreadableBotsDir(t *testing.T) {
	srv := &Server{BotsDir: filepath.Join(t.TempDir(), "does-not-exist")}
	rec := httptest.NewRecorder()
	srv.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// Empty list, never null — the UI maps over it.
	if got := rec.Body.String(); !strings.Contains(got, `"memory_recall_bots":[]`) {
		t.Errorf("want an empty array, got %s", got)
	}
}

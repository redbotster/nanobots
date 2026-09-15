package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The router and the handler have to agree about what the path is.
//
// http.ServeMux matches on the escaped path, so "..%2f..%2fetc" is a single
// segment and binds to {id}; r.PathValue then hands the handler the decoded
// "../../etc". Confirmed against the real mux — this is not a hypothetical
// about what some proxy in front might do.
func TestAPathSegmentIsNotAPath(t *testing.T) {
	mux := http.NewServeMux()
	var got string
	mux.HandleFunc("POST /api/bots/{id}/x", func(w http.ResponseWriter, r *http.Request) {
		got = r.PathValue("id")
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/..%2f..%2fetc/x", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("the route did not even match (%d) — if the mux starts rejecting "+
			"these, catalogBotDir is belt to a working brace, not the only guard", rec.Code)
	}
	if got != "../../etc" {
		t.Fatalf("PathValue = %q, want the decoded traversal — this test exists to "+
			"notice if the stdlib's behaviour changes", got)
	}
}

func TestCatalogBotDirRefusesAnythingThatLeavesBotsDir(t *testing.T) {
	srv := testServer(t)
	srv.BotsDir = t.TempDir()

	for _, id := range []string{
		"../../etc",
		"..",
		".",
		"../sibling",
		"a/b",
		`a\b`,
		"",
		"nested/../../escape",
	} {
		if dir, err := srv.catalogBotDir(id); err == nil {
			t.Errorf("catalogBotDir(%q) = %q, want an error", id, dir)
		}
	}
}

func TestCatalogBotDirAllowsAnOrdinaryID(t *testing.T) {
	srv := testServer(t)
	srv.BotsDir = t.TempDir()

	dir, err := srv.catalogBotDir("receipt-filer")
	if err != nil {
		t.Fatalf("catalogBotDir: %v", err)
	}
	if filepath.Base(dir) != "receipt-filer" {
		t.Errorf("dir = %q", dir)
	}
}

// The endpoint that rewrites nanobot.yaml. Before catalogBotManifest this
// reached a manifest in a sibling directory of BotsDir and rewrote it — the
// id went from PathValue into filepath.Join with nothing in between.
func TestSetBotServiceConnectionCannotWriteOutsideBotsDir(t *testing.T) {
	srv := testServerWithBotsCopy(t)

	// A manifest one level up from BotsDir, where a real install keeps
	// examples/, state/ and the repo itself.
	outside := filepath.Join(filepath.Dir(srv.BotsDir), "outside-the-catalog")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "nanobot.yaml")
	original, err := os.ReadFile(filepath.Join(srv.BotsDir, "receipt-filer", "nanobot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, original, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/api/bots/..%2foutside-the-catalog/services/gmail/connection",
		strings.NewReader(`{"live":false}`)))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	after, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Error("a manifest outside BotsDir was rewritten")
	}
}

// Same shape on the instructions endpoint, which also writes. It used to
// pass filepath.Base(botID), which reads as a guard and is not one:
// filepath.Base("..") is "..", so BotsDir/../nanobot.yaml was reachable.
func TestSetBotInstructionsCannotWriteOutsideBotsDir(t *testing.T) {
	srv := testServerWithBotsCopy(t)

	parent := filepath.Dir(srv.BotsDir)
	victim := filepath.Join(parent, "nanobot.yaml")
	original, err := os.ReadFile(filepath.Join(srv.BotsDir, "post-publisher", "nanobot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, original, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/api/bots/../instructions",
		strings.NewReader(`{"instructions":"owned"}`)))
	// The mux may 301 this one rather than route it; either way the file
	// must be untouched, which is the property under test.
	after, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Errorf("a manifest outside BotsDir was rewritten (status %d)", rec.Code)
	}
}

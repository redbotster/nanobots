package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	orig := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = orig })
	return NewClient("ghp_test")
}

func TestIssuesListFiltersOutPullRequests(t *testing.T) {
	var gotAuth, gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/redbotster/nanobots/issues", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "real issue", "html_url": "https://x/1", "created_at": "2026-01-01T00:00:00Z", "user": map[string]string{"login": "alice"}},
			{"number": 2, "title": "a PR", "html_url": "https://x/2", "created_at": "2026-01-02T00:00:00Z", "user": map[string]string{"login": "bob"}, "pull_request": map[string]string{"url": "https://x/pr/2"}},
		})
	})
	c := testClient(t, mux)

	issues, err := c.IssuesList("redbotster/nanobots", 10)
	if err != nil {
		t.Fatalf("IssuesList: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly 1 (PR filtered out)", issues)
	}
	if issues[0].Number != 1 || issues[0].User != "alice" {
		t.Errorf("issues[0] = %+v", issues[0])
	}
	if gotAuth != "Bearer ghp_test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotPath != "/repos/redbotster/nanobots/issues" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestIssuesListReturnsErrorOnHTTPFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/x/y/issues", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := testClient(t, mux)

	if _, err := c.IssuesList("x/y", 10); err == nil {
		t.Fatal("expected an error for a 404 response")
	}
}

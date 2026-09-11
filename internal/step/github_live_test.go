package step

import (
	"errors"
	"testing"

	"github.com/redbotster/nanobots/internal/github"
)

type fakeGitHubAPI struct {
	result  []github.Issue
	err     error
	gotRepo string
	gotMax  int
}

func (f *fakeGitHubAPI) IssuesList(repo string, max int) ([]github.Issue, error) {
	f.gotRepo, f.gotMax = repo, max
	return f.result, f.err
}

func TestDispatchGitHubIssuesListShapesResultAsWalkableJSON(t *testing.T) {
	f := &fakeGitHubAPI{result: []github.Issue{{Number: 7, Title: "bug", User: "alice"}}}
	out, err := dispatchGitHub(f, "issues.list", map[string]any{"repo": "redbotster/nanobots", "max": float64(5)})
	if err != nil {
		t.Fatalf("dispatchGitHub: %v", err)
	}
	if f.gotRepo != "redbotster/nanobots" || f.gotMax != 5 {
		t.Errorf("fake got repo=%q max=%d", f.gotRepo, f.gotMax)
	}
	items, ok := out.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("out = %#v, want []any of length 1", out)
	}
	m, ok := items[0].(map[string]any)
	if !ok || m["number"] != float64(7) || m["title"] != "bug" {
		t.Errorf("items[0] = %#v", items[0])
	}
}

func TestDispatchGitHubPropagatesError(t *testing.T) {
	f := &fakeGitHubAPI{err: errors.New("boom")}
	if _, err := dispatchGitHub(f, "issues.list", map[string]any{"repo": "x/y"}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestDispatchGitHubUnsupportedOpErrors(t *testing.T) {
	if _, err := dispatchGitHub(&fakeGitHubAPI{}, "nonsense.op", nil); err == nil {
		t.Fatal("expected an error for an unrecognized op")
	}
}

func TestGitHubClientErrorsWhenNotConfigured(t *testing.T) {
	l := &LiveDeps{}
	if _, err := l.githubClient(); err == nil {
		t.Fatal("expected an error when GitHubConfig is unset")
	}
}

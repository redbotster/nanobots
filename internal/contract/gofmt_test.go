package contract

import (
	"os/exec"
	"strings"
	"testing"
)

// Unformatted Go compiles, vets clean, and passes every other test here.
//
// Which is how two mis-indented lines survived a full verification pass and
// sat in internal/api/connections.go across several commits: a scripted edit
// inserted them at the wrong depth inside a nested block, and nothing in
// `go build ./... && go vet ./... && go test ./... -race` has any opinion
// about whitespace. It was found by eye, days later.
//
// gofmt is the one style question in Go with a single right answer, so it is
// the kind of claim this package already exists to check — the same reason
// the README's own test count is a test.
func TestEveryGoFileIsFormatted(t *testing.T) {
	root := repoRoot(t)

	// gofmt -l prints the files whose formatting differs; silence is a pass.
	// Run over the tree rather than a package list so cmd/, internal/ and
	// anything added later are all covered without a list to maintain.
	out, err := exec.Command("gofmt", "-l", root).Output()
	if err != nil {
		t.Skipf("gofmt not runnable here: %v", err)
	}
	var unformatted []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			unformatted = append(unformatted, strings.TrimPrefix(line, root+"/"))
		}
	}
	if len(unformatted) > 0 {
		t.Errorf("gofmt would change %d file(s) — run `gofmt -w .`:\n  %s",
			len(unformatted), strings.Join(unformatted, "\n  "))
	}
}

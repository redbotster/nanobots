package contract

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The README states how many tests this repo has, and prints the command
// that produces the number so a reader can check it. That number drifted
// twice in one day of work — a claim nobody can be expected to
// hand-maintain is a claim that will be wrong. This makes the repo check
// its own README.
func TestReadmeTestCountIsAccurate(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^(\d+) table-driven Go tests`).FindSubmatch(readme)
	if m == nil {
		t.Skip("README no longer states a test count")
	}
	claimed, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}

	// Exactly the command the README prints, so the two can't disagree.
	cmd := exec.Command("bash", "-c",
		`grep -rho '^func Test[A-Za-z0-9_]*' --include='*_test.go' . | sort -u | wc -l`)
	cmd.Dir = root // the README's command is written to be run from the repo root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("could not count tests here: %v", err)
	}
	actual, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Skipf("unexpected count output %q", out)
	}

	if claimed != actual {
		t.Errorf("README claims %d tests; there are %d. Update the number in README.md.", claimed, actual)
	}
}

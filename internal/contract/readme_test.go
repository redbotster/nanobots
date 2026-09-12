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

// The README also counts bots and swarms. Same reasoning as the test
// count, and the same outcome without a check: both had drifted — 30 bots
// where there were 33, 14 swarms where there were 15 — because three
// review bots and a supervisor swarm were added without anyone
// re-counting. A number in prose is a claim, and an unchecked claim rots.
func TestReadmeCatalogCountsAreAccurate(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		what    string
		pattern string
		count   func() int
	}{
		{
			"bots", `(?m)^\*\*(\d+) bots\*\*`,
			func() int { return len(allBotIDs(t, root)) },
		},
		{
			"swarms", `(?m)^\*\*(\d+) swarms\*\*`,
			func() int {
				files, _ := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
				return len(files)
			},
		},
	} {
		m := regexp.MustCompile(tc.pattern).FindSubmatch(readme)
		if m == nil {
			t.Errorf("README no longer states a %s count in the expected form", tc.what)
			continue
		}
		claimed, err := strconv.Atoi(string(m[1]))
		if err != nil {
			t.Fatal(err)
		}
		if actual := tc.count(); claimed != actual {
			t.Errorf("README claims %d %s; there are %d.", claimed, tc.what, actual)
		}
	}
}

// The README's swarm table is the catalog's front door, and a swarm nobody
// added a row for is a swarm nobody finds. supervisor-review shipped
// without one.
func TestReadmeNamesEverySwarm(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no swarms found: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		if !strings.Contains(string(readme), "`"+name+"`") {
			t.Errorf("swarm %q has no row in the README's table", name)
		}
	}
}

// Same for bots: the catalog list is how someone discovers what can snap
// into what, and the AI composer's prompt is built from the same directory.
func TestReadmeNamesEveryBot(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, id := range allBotIDs(t, root) {
		if !strings.Contains(string(readme), "`"+id+"`") {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Errorf("these bots have no mention in the README: %s", strings.Join(missing, ", "))
	}
}

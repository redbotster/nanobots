package contract

import (
	"fmt"
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

// Every page in docs/ should be reachable from the README. docs/memory.md
// was the one that wasn't — written, linked from other docs, and invisible
// to anyone starting at the front page.
func TestReadmeLinksEveryDoc(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if !strings.Contains(string(readme), "docs/"+e.Name()) {
			missing = append(missing, e.Name())
		}
	}
	if len(missing) > 0 {
		t.Errorf("these docs are never linked from the README, so nobody starting at the front"+
			" page will find them: %s", strings.Join(missing, ", "))
	}
}

// "Eleven of the fifteen catalog swarms carry a cron trigger" is repeated
// in five files — a CLI help note, the service installer's doc comment, the
// builder, and twice in docs/scheduler.md. It was "fourteen" in all of them
// for a long time, and wrong: the real split is 11 cron, 2 event, 1 webhook,
// 1 manual. A number nobody can check is a number that drifts.
func TestTheClaimedCronSwarmCountIsAccurate(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "examples", "swarms"))
	if err != nil {
		t.Fatal(err)
	}
	cron, total := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		total++
		raw, err := os.ReadFile(filepath.Join(root, "examples", "swarms", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "type: cron") {
			cron++
		}
	}

	// Sentence case: the count opens a sentence, the total does not.
	claim := fmt.Sprintf("%s of the %s catalog swarms carry a cron trigger",
		strings.ToUpper(numberWord(cron)[:1])+numberWord(cron)[1:], numberWord(total))
	for _, f := range []string{
		"cmd/nanobots/main.go",
		"internal/service/service.go",
		"internal/api/builder.go",
		"docs/scheduler.md",
	} {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		// The comment wraps, so compare on whitespace-collapsed text.
		flat := strings.Join(strings.Fields(strings.ReplaceAll(string(raw), "//", " ")), " ")
		if !strings.Contains(flat, claim) {
			t.Errorf("%s does not say %q — there are %d cron swarms of %d", f, claim, cron, total)
		}
	}
}

// numberWord spells the small numbers these comments use.
func numberWord(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven",
		"eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
		"sixteen", "seventeen", "eighteen", "nineteen", "twenty"}
	if n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

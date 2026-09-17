package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/planner"
)

// docs/parallelism.md claims how many catalog swarms actually benefit from
// running a wave concurrently, and lists them in a table.
//
// It said "Six of the fifteen" over a five-row table, and the README and a
// planner test comment both repeated the six. The real answer is five of
// sixteen — a swarm was added, another was rewired into a chain, and three
// files kept the old sentence. Exactly the drift the cron-count test exists
// to stop, one claim over.
//
// This checks the sentence, the leftover count, and that the table names the
// same swarms the planner actually finds, so a new branching swarm fails
// here rather than being quietly missing from the page.
func TestTheClaimedWideSwarmCountIsAccurate(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "examples", "swarms", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no swarms found: %v", err)
	}

	var wide []string
	for _, f := range files {
		result, err := planner.Plan(f, filepath.Join(root, "bots"))
		if err != nil {
			t.Fatalf("plan %s: %v", filepath.Base(f), err)
		}
		levels, err := result.DAG.Levels()
		if err != nil {
			t.Fatalf("levels %s: %v", filepath.Base(f), err)
		}
		for _, level := range levels {
			if len(level) > 1 {
				wide = append(wide, strings.TrimSuffix(filepath.Base(f), ".yaml"))
				break
			}
		}
	}

	page := filepath.Join(root, "docs", "parallelism.md")
	raw, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	claim := fmt.Sprintf("%s of the %s catalog swarms have a wave wider than one",
		strings.ToUpper(numberWord(len(wide))[:1])+numberWord(len(wide))[1:], numberWord(len(files)))
	if !strings.Contains(doc, claim) {
		t.Errorf("docs/parallelism.md does not say %q — %d of %d swarms branch",
			claim, len(wide), len(files))
	}
	rest := fmt.Sprintf("The other %s are straight chains", numberWord(len(files)-len(wide)))
	if !strings.Contains(doc, rest) {
		t.Errorf("docs/parallelism.md does not say %q", rest)
	}
	for _, name := range wide {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("docs/parallelism.md's table has no row for %q, which does branch", name)
		}
	}
}

package contract

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

// A bot that stops to ask a person has to budget for a person.
//
// `max_runtime_secs` is the bot's own ceiling, and the runner kills it there
// whether the bot is working or waiting. So a bot with an `approve` step and
// a 60-second budget gives you one minute to answer, and then reports a
// failure — which is what content-engine did, four times in two days, on a
// question that read "Publish this post to X and LinkedIn?". The swarm card
// says "pauses for you"; the pause was a minute long.
//
// Three of the catalog's four approving bots already used 1800, which is
// exactly runner.ApprovalTimeout. post-publisher used 60 and was the odd one
// out — and the one publishing to two real accounts.
//
// This is the second time. docs/testing.md still records the first: a browser
// pass found "a pre-existing email-drive-file timeout bug whose
// max_runtime_secs: 60 guardrail was too short for its own approval gate to
// ever be answered in time". That bot was fixed; the identical bug two
// directories over was not noticed, and went on failing real runs. Which is
// the argument for a test rather than a second careful reading.
//
// Checked against the constant rather than a number written here, so the two
// cannot drift: raising the approval window without raising the bots' budgets
// would silently recreate the same failure.
func TestABotThatAsksAPersonWaitsLongEnoughForOne(t *testing.T) {
	root := repoRoot(t)
	checked := 0

	for _, id := range allBotIDs(t, root) {
		path := filepath.Join(root, "bots", id, "nanobot.yaml")
		nb, err := schema.LoadNanobot(path)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		asks := false
		for _, s := range nb.Spec.Steps {
			if s.Type == "approve" {
				asks = true
				break
			}
		}
		if !asks {
			continue
		}
		checked++

		budget := time.Duration(nb.Spec.Guardrails.MaxRuntimeSecs) * time.Second
		if budget == 0 {
			// No declared ceiling is not this test's business — whatever the
			// runner's own default is, it is not a per-bot claim that a
			// person has N seconds to answer.
			continue
		}
		if budget < runner.ApprovalTimeout {
			t.Errorf("bots/%s stops to ask a person and then kills itself after %s, "+
				"but an approval stays answerable for %s — the run fails with the question "+
				"still on screen. Give it at least %s.",
				id, budget, runner.ApprovalTimeout, runner.ApprovalTimeout)
		}
	}

	if checked == 0 {
		t.Error("no bot in the catalog has an approve step, so this test checked nothing")
	}
	// The file this is really about, named so a rename does not quietly
	// reduce this to a tautology.
	if _, err := os.Stat(filepath.Join(root, "bots", "post-publisher", "nanobot.yaml")); err != nil {
		t.Errorf("post-publisher is gone: %v", err)
	}
}

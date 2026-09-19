package runner

import (
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

// A watch that finds nothing new must leave a quiet, successful run.
//
// The three outcomes a bot can have that produce no outputs are easy to
// confuse, and telling them apart is most of what reading a run is for:
//
//	nothing to do   looked, nothing had changed         -> run succeeds
//	tolerated fail  broke, and the swarm said carry on  -> run succeeds, warns
//	fatal           broke                               -> run fails
//
// An hourly watch should read as twenty-four quiet runs. Recording it as a
// tolerated failure would be twenty-four warnings a day for a system working
// exactly as intended, which is how people learn to ignore warnings.
func TestAWatchWithNothingToDoSkipsWhatIsBehindItAndSucceeds(t *testing.T) {
	rs := swarmWith(
		[]schema.BotRef{{ID: "watcher"}, {ID: "notes"}, {ID: "followups"}},
		[]schema.Snap{
			{From: "watcher.file", To: "notes.file"},
			{From: "notes.summary", To: "followups.text"},
		},
	)
	run := NewRun("watch-probe")

	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "watcher" {
			return &NothingToDoError{Bot: "watcher", Reason: "no new file since the last run"}
		}
		t.Errorf("%s ran, though its inputs never arrived", id)
		return nil
	}}

	err := o.runDAG(run, rs)
	if err != nil {
		t.Fatalf("a watch with nothing to do failed the run: %v", err)
	}
	if n := len(run.GetTolerated()); n != 0 {
		t.Errorf("%d tolerated failure(s) recorded; nothing failed", n)
	}

	var lines []string
	for _, e := range run.LogEntries() {
		lines = append(lines, e.Bot+": "+e.Msg)
	}
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "watcher: nothing to do — no new file since the last run") {
		t.Errorf("the run never said why it was quiet:\n%s", joined)
	}
	// Said once, the same way, however far down the chain. Each hop used to
	// add its own prefix, so the third bot read
	// "skipped — notes: skipped: watcher: nothing to do: …".
	for _, bot := range []string{"notes", "followups"} {
		want := bot + ": skipped — watcher had nothing to do: no new file since the last run"
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

// Retrying a watch that found nothing would poll the same folder three
// times and reach the same answer.
func TestNothingToDoIsNotRetried(t *testing.T) {
	if worthRetrying(&NothingToDoError{Bot: "watcher", Reason: "nothing new"}) {
		t.Error("a watch with nothing to do would be retried")
	}
}

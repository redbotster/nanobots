package runner

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

// resolvedFor builds the minimum ResolvedSwarm runLevels reads: it only
// looks bots up by id.
func resolvedFor(ids ...string) *planner.ResolvedSwarm {
	refs := make([]schema.BotRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, schema.BotRef{ID: id})
	}
	return swarmWith(refs, nil)
}

// Bots in one wave have no path between them in the DAG, so they can run at
// the same time — which is the entire point. If they were still serialised
// the change would be a no-op with extra machinery, and nothing else here
// would catch that.
func TestBotsInAWaveRunAtTheSameTime(t *testing.T) {
	var running, peak int32
	var mu sync.Mutex

	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
		n := atomic.AddInt32(&running, 1)
		mu.Lock()
		if n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return nil
	}}

	run := NewRun("probe")
	err := o.runLevels(run, resolvedFor("a", "b", "c"), [][]string{{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d — the wave ran one bot at a time", peak)
	}
}

// The other half of the contract: a later wave must not start until the
// earlier one has finished, because that is the only thing stopping a bot
// from reading an output that doesn't exist yet.
func TestALaterWaveWaitsForTheEarlierOne(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var inFlight int32
	var overlapped bool

	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		mu.Lock()
		order = append(order, "start:"+id)
		mu.Unlock()
		atomic.AddInt32(&inFlight, 1)
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return nil
	}}

	// second must never begin while either first-wave bot is still going.
	o.runBotFn = func(run *Run, rs *planner.ResolvedSwarm, id string, rb *planner.ResolvedBot) error {
		if id == "second" && atomic.LoadInt32(&inFlight) > 0 {
			mu.Lock()
			overlapped = true
			mu.Unlock()
		}
		atomic.AddInt32(&inFlight, 1)
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		mu.Lock()
		order = append(order, id)
		mu.Unlock()
		return nil
	}

	err := o.runLevels(NewRun("probe"), resolvedFor("a", "b", "second"),
		[][]string{{"a", "b"}, {"second"}})
	if err != nil {
		t.Fatal(err)
	}
	if overlapped {
		t.Error("a second-wave bot started while the first wave was still running")
	}
	if order[len(order)-1] != "second" {
		t.Errorf("order = %v, want second last", order)
	}
}

// When two bots in a wave fail, both errors matter. A swarm's independent
// branches usually fail for one shared reason — an expired credential, a
// stopped Docker — and reporting whichever lost the race sends someone
// looking for two bugs.
func TestEveryFailureInAWaveIsReported(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "ok" {
			return nil
		}
		return fmt.Errorf("%s could not reach the vault", id)
	}}

	err := o.runLevels(NewRun("probe"), resolvedFor("alpha", "ok", "beta"),
		[][]string{{"alpha", "ok", "beta"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"alpha", "beta", "vault"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
	// Deterministic ordering: the message must not depend on which
	// goroutine finished first.
	if a, b := strings.Index(err.Error(), "alpha"), strings.Index(err.Error(), "beta"); a > b {
		t.Errorf("failures are not in swarm order: %v", err)
	}
}

// A single failure has to read exactly as it always did — a swarm that
// fails one bot is the common case, and its message is what every existing
// test and every run in history looks like.
func TestASingleFailureReadsUnchanged(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "bad" {
			return fmt.Errorf("container exited 1")
		}
		return nil
	}}

	err := o.runLevels(NewRun("probe"), resolvedFor("bad"), [][]string{{"bad"}})
	if err == nil || err.Error() != "container exited 1" {
		t.Errorf("err = %v, want the bot's own error verbatim", err)
	}

	// And the same when it fails alongside a healthy sibling.
	err = o.runLevels(NewRun("probe"), resolvedFor("bad", "good"), [][]string{{"bad", "good"}})
	if err == nil || err.Error() != "container exited 1" {
		t.Errorf("err = %v, want the bot's own error verbatim", err)
	}
}

// Cancelling a wave half-way would orphan a container that was already
// starting — a leak this runner has had once before. So a failing wave is
// allowed to finish, and only then does the run stop.
func TestAFailingWaveStillLetsItsSiblingsFinish(t *testing.T) {
	var finished int32
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "fails" {
			return fmt.Errorf("boom")
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&finished, 1)
		return nil
	}}

	err := o.runLevels(NewRun("probe"), resolvedFor("fails", "slow1", "slow2"),
		[][]string{{"fails", "slow1", "slow2"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&finished); got != 2 {
		t.Errorf("%d siblings finished, want 2 — a failure cancelled work mid-container", got)
	}
}

// A later wave must not run after an earlier one failed.
func TestNoLaterWaveRunsAfterAFailure(t *testing.T) {
	var ranLater bool
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "later" {
			ranLater = true
		}
		if id == "first" {
			return fmt.Errorf("boom")
		}
		return nil
	}}

	if err := o.runLevels(NewRun("probe"), resolvedFor("first", "later"),
		[][]string{{"first"}, {"later"}}); err == nil {
		t.Fatal("expected an error")
	}
	if ranLater {
		t.Error("a later wave ran after an earlier one failed")
	}
}

// Concurrency is capped because each bot is a container: four
// Chromium-bearing ones already want a couple of gigabytes, and a laptop
// that starts swapping finishes slower than it would have sequentially.
func TestConcurrencyIsCapped(t *testing.T) {
	var running, peak int32
	var mu sync.Mutex
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
		n := atomic.AddInt32(&running, 1)
		mu.Lock()
		if n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return nil
	}}

	var wide []string
	for i := 0; i < maxParallelBots*3; i++ {
		wide = append(wide, fmt.Sprintf("bot%02d", i))
	}
	if err := o.runLevels(NewRun("probe"), resolvedFor(wide...), [][]string{wide}); err != nil {
		t.Fatal(err)
	}
	if int(peak) > maxParallelBots {
		t.Errorf("peak concurrency was %d, cap is %d", peak, maxParallelBots)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d — nothing ran in parallel at all", peak)
	}
}

// swarmWith builds a ResolvedSwarm with real bot refs and snaps, so
// runLevels sees the on_error values and dependency edges it reads.
func swarmWith(bots []schema.BotRef, snaps []schema.Snap) *planner.ResolvedSwarm {
	rs := &planner.ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{Bots: bots, Snaps: snaps}},
		Bots:  map[string]*planner.ResolvedBot{},
	}
	for _, b := range bots {
		rs.Bots[b.ID] = &planner.ResolvedBot{Nanobot: &schema.Nanobot{
			Metadata: schema.Metadata{Name: b.ID, Version: "0.1.0"},
		}}
	}
	return rs
}

// The failure this exists for: get-paid sent every reminder, then failed
// the whole run because it couldn't post a Slack summary nothing reads.
func TestASwarmContinuesPastABotMarkedContinue(t *testing.T) {
	run := NewRun("get-paid")
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		if id == "notifier" {
			return fmt.Errorf("Secret slack/bot_token not found")
		}
		return nil
	}}
	rs := swarmWith([]schema.BotRef{
		{ID: "sender"},
		{ID: "notifier", OnError: schema.OnErrorContinue},
	}, nil)

	if err := o.runLevels(run, rs, [][]string{{"sender"}, {"notifier"}}); err != nil {
		t.Fatalf("the run failed despite on_error: continue: %v", err)
	}
	tol := run.GetTolerated()
	if len(tol) != 1 || tol[0].Bot != "notifier" {
		t.Fatalf("tolerated = %+v, want the notifier recorded", tol)
	}
	if !strings.Contains(tol[0].Error, "slack") {
		t.Errorf("the reason was lost: %q", tol[0].Error)
	}
}

// Default is stop. Silence must mean "fail loudly" — continuing has to be
// something someone chose, not something they got.
func TestTheDefaultIsStillToFailTheRun(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		return fmt.Errorf("boom")
	}}
	for _, policy := range []string{"", schema.OnErrorStop} {
		run := NewRun("probe")
		rs := swarmWith([]schema.BotRef{{ID: "only", OnError: policy}}, nil)
		if err := o.runLevels(run, rs, [][]string{{"only"}}); err == nil {
			t.Errorf("on_error=%q did not fail the run", policy)
		}
		if len(run.GetTolerated()) != 0 {
			t.Errorf("on_error=%q recorded a tolerated failure", policy)
		}
	}
}

// Continuing past a bot does not mean pretending it produced anything.
// Without this, one tolerated failure cascades into a run of "upstream bot
// has no recorded outputs yet" from every bot behind it.
func TestBotsDownstreamOfAToleratedFailureAreSkipped(t *testing.T) {
	var ran []string
	var mu sync.Mutex
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		mu.Lock()
		ran = append(ran, id)
		mu.Unlock()
		if id == "middle" {
			return fmt.Errorf("boom")
		}
		return nil
	}}
	rs := swarmWith(
		[]schema.BotRef{
			{ID: "first"},
			{ID: "middle", OnError: schema.OnErrorContinue},
			{ID: "last"},
			{ID: "unrelated"},
		},
		[]schema.Snap{
			{From: "first.out", To: "middle.in"},
			{From: "middle.out", To: "last.in"},
		},
	)

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"first"}, {"middle", "unrelated"}, {"last"}}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	for _, id := range ran {
		if id == "last" {
			t.Error("a bot downstream of a failed one was run against missing inputs")
		}
	}
	// A bot that never needed it still runs — skipping is about the data,
	// not about the wave.
	var sawUnrelated bool
	for _, id := range ran {
		if id == "unrelated" {
			sawUnrelated = true
		}
	}
	if !sawUnrelated {
		t.Error("an unrelated bot was skipped too")
	}
	// And the skip is in the log, not silent.
	var said bool
	for _, l := range run.LogEntries() {
		if l.Bot == "last" && strings.Contains(l.Msg, "skipped") {
			said = true
		}
	}
	if !said {
		t.Errorf("nothing in the log says why 'last' never ran: %+v", run.LogEntries())
	}
}

// Skipping is transitive: a bot behind a skipped bot is skipped too,
// without needing its own edge to the original failure.
func TestSkippingIsTransitive(t *testing.T) {
	var ran []string
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		ran = append(ran, id)
		if id == "a" {
			return fmt.Errorf("boom")
		}
		return nil
	}}
	rs := swarmWith(
		[]schema.BotRef{{ID: "a", OnError: schema.OnErrorContinue}, {ID: "b"}, {ID: "c"}},
		[]schema.Snap{{From: "a.out", To: "b.in"}, {From: "b.out", To: "c.in"}},
	)
	if err := o.runLevels(NewRun("probe"), rs, [][]string{{"a"}, {"b"}, {"c"}}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "a" {
		t.Errorf("ran = %v, want only a", ran)
	}
}

// A fatal failure alongside a tolerated one in the same wave still fails
// the run — "continue" is per bot, not a mood the whole wave catches.
func TestOneToleratedFailureDoesNotExcuseAFatalOne(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, _ *planner.ResolvedBot) error {
		return fmt.Errorf("boom in %s", id)
	}}
	run := NewRun("probe")
	rs := swarmWith([]schema.BotRef{
		{ID: "soft", OnError: schema.OnErrorContinue},
		{ID: "hard"},
	}, nil)

	err := o.runLevels(run, rs, [][]string{{"soft", "hard"}})
	if err == nil {
		t.Fatal("the run survived a fatal failure")
	}
	if strings.Contains(err.Error(), "soft") {
		t.Errorf("the tolerated bot was reported as fatal: %v", err)
	}
	if len(run.GetTolerated()) != 1 {
		t.Errorf("the tolerated failure was lost: %+v", run.GetTolerated())
	}
}

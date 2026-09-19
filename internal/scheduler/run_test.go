package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The whole point of moving off a fixed poll: a swarm added while nanobotd
// is already running gets picked up because something told the scheduler,
// not because it happened to look again. Proven by a real Run() loop and a
// real fsnotify watch, waiting far less than the old 20-second poll
// interval — a poll-based scheduler could pass this test too, just slowly;
// what this actually distinguishes is answered by
// TestRunDoesNotPollOnAFixedTimerWhenIdle below.
func TestRunFiresOnAFileAddedAfterStarting(t *testing.T) {
	dir := t.TempDir()
	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Give Run its first tick and its watch a moment to establish before
	// writing the file — otherwise this can race the initial setup rather
	// than testing the event path.
	time.Sleep(50 * time.Millisecond)
	path := writeSwarm(t, dir, "fresh.yaml", "* * * * *") // every minute — already due on sight

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := orch.executions(); len(got) == 1 && got[0] == path {
			return // fired
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("a swarm added after Run() started was not fired within 2s (fsnotify event never arrived, or Run never reacted): executions = %v", orch.executions())
}

// The other half of "event-driven, not polling": with nothing due and
// nothing changing, Run must not be waking up on a fixed short interval to
// find nothing to do. Given a schedule that only fires once a year, a
// poll-based scheduler ticking every 20s would have rescanned dozens of
// times in the window this test waits; an event-driven one rescans once,
// at startup, and then only sleeps until fsnotify says something changed
// or the (far later) next fire time arrives.
func TestRunDoesNotPollOnAFixedTimerWhenIdle(t *testing.T) {
	dir := t.TempDir()
	writeSwarm(t, dir, "yearly.yaml", "0 0 1 1 *") // once a year — never due in this test's window
	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	tickCount := 0
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}

	// Wrap tick via Now: each call to Now() happens once per loop
	// iteration (tick's own timestamp), so counting Now() calls counts
	// rescans without needing to export anything new.
	s.Now = func() time.Time { tickCount++; return time.Now() }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	time.Sleep(500 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond) // let Run observe cancellation

	// One rescan at startup. Anything approaching "500ms / 20s poll" worth
	// of extra rescans would mean this is still polling on a short timer.
	if tickCount > 3 {
		t.Errorf("Now() (one call per rescan) was called %d times in 500ms with nothing due and nothing changing — looks like polling, not event-driven", tickCount)
	}
}

func TestWaitReturnsZeroWhenSomethingIsAlreadyDue(t *testing.T) {
	s := &Scheduler{state: map[string]*swarmState{
		"a": {nextFire: time.Now().Add(-time.Minute)},
	}}
	if got := s.wait(time.Now()); got != 0 {
		t.Errorf("wait() = %s, want 0 for an already-due swarm", got)
	}
}

func TestWaitReturnsTheEarliestNextFireAcrossSwarms(t *testing.T) {
	now := time.Now()
	s := &Scheduler{state: map[string]*swarmState{
		"far":     {nextFire: now.Add(10 * time.Minute)},
		"nearest": {nextFire: now.Add(2 * time.Minute)},
		"mid":     {nextFire: now.Add(5 * time.Minute)},
	}}
	got := s.wait(now)
	if got < 90*time.Second || got > 2*time.Minute {
		t.Errorf("wait() = %s, want ~2 minutes (the nearest of the three)", got)
	}
}

func TestWaitReturnsMaxWaitWhenNothingIsScheduled(t *testing.T) {
	s := &Scheduler{state: map[string]*swarmState{}}
	if got := s.wait(time.Now()); got != maxWait {
		t.Errorf("wait() = %s, want maxWait (%s) when nothing is tracked", got, maxWait)
	}
}

// fsnotify can't watch a directory that doesn't exist. That must not mean
// the scheduler never fires anything again — it falls back to polling,
// which will pick the directory up once it exists (the same behavior
// os.ReadDir-based tick already has: it just returns nothing until then).
func TestRunFallsBackToPollingWhenTheDirectoryCannotBeWatched(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist-yet")
	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{
		Orchestrator: orch,
		Runs:         runs,
		SwarmsDir:    missing,
		PollInterval: 30 * time.Millisecond, // fast, so the test doesn't wait 20s
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Now create the directory and a due swarm — the polling fallback
	// should pick it up on its next tick.
	time.Sleep(20 * time.Millisecond)
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeSwarm(t, missing, "now.yaml", "* * * * *")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := orch.executions(); len(got) == 1 && got[0] == path {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the polling fallback never fired the swarm: executions = %v", orch.executions())
}

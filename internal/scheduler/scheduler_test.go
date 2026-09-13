package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
)

type fakeOrchestrator struct {
	mu       sync.Mutex
	executed []string
	err      error
}

func (f *fakeOrchestrator) ExecuteSwarm(swarmPath string) (*runner.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.executed = append(f.executed, swarmPath)
	return runner.NewRun(filepath.Base(swarmPath)), nil
}

func (f *fakeOrchestrator) executions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.executed...)
}

type fakeRunStore struct {
	mu   sync.Mutex
	runs []*runner.Run
}

func (f *fakeRunStore) Add(r *runner.Run) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, r)
}

func (f *fakeRunStore) List() []*runner.Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*runner.Run(nil), f.runs...)
}

func (f *fakeRunStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

func writeSwarm(t *testing.T, dir, name, cronExpr string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: ` + name + `
spec:
  trigger:
    type: cron
    expr: "` + cronExpr + `"
  bots: []
  snaps: []
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTickFiresADueSwarmAndAdvancesItsNextFire(t *testing.T) {
	dir := t.TempDir()
	// An hourly schedule (not "every minute") so discovering it partway
	// through an hour doesn't itself already match — isolates "does it
	// fire at its next real occurrence" from the separate
	// fires-immediately-if-already-due behavior covered by
	// TestTickFiresImmediatelyWhenAlreadyDueOnFirstSight below.
	path := writeSwarm(t, dir, "hourly.yaml", "0 * * * *")

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}

	base := time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC)
	s.tick(base) // establishes next fire at 10:00, doesn't fire yet
	if len(orch.executions()) != 0 {
		t.Fatalf("expected no execution yet, got %v", orch.executions())
	}

	due := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	s.tick(due) // now == next fire — due
	if got := orch.executions(); len(got) != 1 || got[0] != path {
		t.Fatalf("executions = %v, want [%s]", got, path)
	}
	if runs.count() != 1 {
		t.Fatalf("runs added = %d, want 1", runs.count())
	}

	s.tick(due) // same instant again — must not re-fire
	if len(orch.executions()) != 1 {
		t.Fatalf("expected no double-fire, executions = %v", orch.executions())
	}

	s.tick(due.Add(time.Hour)) // next occurrence — fires again
	if len(orch.executions()) != 2 {
		t.Fatalf("expected a second fire at the next occurrence, executions = %v", orch.executions())
	}
}

func TestTickMarksTheRunAsSchedulerTriggered(t *testing.T) {
	dir := t.TempDir()
	writeSwarm(t, dir, "every-minute.yaml", "* * * * *")

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}
	s.tick(time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC))

	runs.mu.Lock()
	defer runs.mu.Unlock()
	if len(runs.runs) != 1 {
		t.Fatalf("runs added = %d, want 1", len(runs.runs))
	}
	if got := runs.runs[0].TriggeredBy; got != "schedule" {
		t.Errorf("TriggeredBy = %q, want \"schedule\"", got)
	}
}

func TestTickFiresImmediatelyWhenAlreadyDueOnFirstSight(t *testing.T) {
	// Discovering (or just having edited) a schedule that already matches
	// this very instant should fire now, not wait a full cycle — a human
	// adding "run every minute" shouldn't have to wait up to a minute (or,
	// worse, up to a day for a daily schedule that happens to match) to
	// see it work once.
	dir := t.TempDir()
	path := writeSwarm(t, dir, "every-minute.yaml", "* * * * *")

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}

	s.tick(time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC))
	if got := orch.executions(); len(got) != 1 || got[0] != path {
		t.Fatalf("executions = %v, want an immediate fire on first sight", got)
	}
}

func TestTickIgnoresNonCronTriggers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manual.yaml")
	content := `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: manual
spec:
  trigger:
    type: manual
  bots: []
  snaps: []
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}
	s.tick(time.Now())
	s.tick(time.Now().Add(24 * time.Hour))
	if len(orch.executions()) != 0 {
		t.Errorf("a manual-trigger swarm should never be scheduled, got %v", orch.executions())
	}
}

func TestTickSkipsABadCronExpressionWithoutBlockingOthers(t *testing.T) {
	dir := t.TempDir()
	writeSwarm(t, dir, "broken.yaml", "not a cron expr")
	goodPath := writeSwarm(t, dir, "good.yaml", "0 * * * *") // hourly, not already-due at :30

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}

	base := time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC)
	s.tick(base)
	s.tick(base.Add(30 * time.Minute)) // 10:00 — good.yaml's next occurrence
	if got := orch.executions(); len(got) != 1 || got[0] != goodPath {
		t.Fatalf("executions = %v, want only [%s]", got, goodPath)
	}
}

func TestTickForgetsARemovedSwarm(t *testing.T) {
	dir := t.TempDir()
	path := writeSwarm(t, dir, "temp.yaml", "* * * * *")

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}
	s.tick(time.Now())

	s.mu.Lock()
	_, tracked := s.state[path]
	s.mu.Unlock()
	if !tracked {
		t.Fatal("expected the swarm to be tracked after its first tick")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.tick(time.Now())
	s.mu.Lock()
	_, stillTracked := s.state[path]
	s.mu.Unlock()
	if stillTracked {
		t.Error("expected a removed swarm to be forgotten")
	}
}

func TestTickPicksUpAnExprChangeWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	writeSwarm(t, dir, "changing.yaml", "0 0 1 1 *") // once a year — won't fire in this test

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{Orchestrator: orch, Runs: runs, SwarmsDir: dir}

	base := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	s.tick(base)

	// Edit the file in place to a schedule that fires every minute — the
	// scheduler should notice on the very next tick, no restart.
	path := writeSwarm(t, dir, "changing.yaml", "* * * * *")
	s.tick(base.Add(time.Minute))
	if got := orch.executions(); len(got) != 1 || got[0] != path {
		t.Fatalf("expected the updated schedule to fire, executions = %v", got)
	}
}

// The whole point of the breaker: a schedule that has failed the same way
// over and over stops costing anything. Before this, support-desk-lite had
// burned roughly twenty hours of container time re-proving that Slack was
// not connected.
func TestTickStopsFiringASwarmThatKeepsFailing(t *testing.T) {
	dir := t.TempDir()
	writeSwarm(t, dir, "hourly.yaml", "0 * * * *")

	orch := &fakeOrchestrator{}
	runs := &fakeRunStore{}
	s := &Scheduler{
		Orchestrator: orch,
		Runs:         runs,
		SwarmsDir:    dir,
		Breaker:      &Breaker{MaxFailures: 2},
	}

	at := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	s.tick(at.Add(-30 * time.Minute)) // establish next fire at 09:00

	// Two hours, two runs, both failing — the fake orchestrator's runs are
	// added to the store, so mark them failed the way a real one would.
	for i := 0; i < 2; i++ {
		s.tick(at.Add(time.Duration(i) * time.Hour))
		for _, r := range runs.List() {
			if r.GetStatus() != runner.StatusFailed {
				r.SetError(errors.New("slack is not connected"))
				r.SetStatus(runner.StatusFailed)
			}
		}
	}
	if len(orch.executions()) != 2 {
		t.Fatalf("expected 2 runs before the breaker trips, got %d", len(orch.executions()))
	}

	// The third hour is due, and must not fire.
	s.tick(at.Add(2 * time.Hour))
	if got := len(orch.executions()); got != 2 {
		t.Errorf("fired %d times; the breaker should have stopped the third", got)
	}

	// Resuming lets it try again.
	if err := s.Breaker.Resume("hourly.yaml"); err != nil {
		t.Fatal(err)
	}
	s.tick(at.Add(3 * time.Hour))
	if got := len(orch.executions()); got != 3 {
		t.Errorf("fired %d times after Resume, want 3 — resume did not take effect", got)
	}
}

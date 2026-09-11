package foundry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeAgent is a test-only Agent: instead of shelling out to a real coding
// CLI, it just performs a scripted filesystem action inside workDir, the
// exact same directory a real Agent would be sandboxed to.
type fakeAgent struct {
	copyFrom  string // a real bot dir to copy in as bots/<newID>/
	newID     string
	extraPath string // optional: an extra file written outside bots/, to simulate a violation
	err       error
	events    []Event
}

func (f *fakeAgent) Run(ctx context.Context, workDir string, in BriefInput, events chan<- Event) error {
	for _, ev := range f.events {
		select {
		case events <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.err != nil {
		return f.err
	}
	if f.extraPath != "" {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(workDir, f.extraPath)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(workDir, f.extraPath), []byte("x"), 0o644); err != nil {
			return err
		}
	}
	if f.copyFrom != "" {
		if err := copyDir(f.copyFrom, filepath.Join(workDir, "bots", f.newID)); err != nil {
			return err
		}
	}
	return nil
}

// realBotFixture resolves the repo's own bots/content-ideas as a
// known-conforming fixture to copy in — it's real, existing, and doesn't
// reference its own directory name anywhere, so renaming the directory it
// lives in doesn't break its conformance.
func realBotFixture(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // internal/foundry
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wd, "..", "..", "bots", "content-ideas")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("repo fixture bot not found at %s: %v", path, err)
	}
	return path
}

func waitForApproval(t *testing.T, job *Job) *approvalHandle {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pending := job.PendingApprovals(); len(pending) > 0 {
			return &approvalHandle{id: pending[0].ID}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the review approval to register")
	return nil
}

type approvalHandle struct{ id string }

// waitForTerminal polls Job's own mutex-guarded Outcome rather than
// runner.Run's plain Status field — Status is written under Run's mutex in
// SetStatus but has no synchronized reader (runner.Run is out of scope for
// this feature to change), so reading it directly here would be a genuine
// data race, not just a style nit. Outcome() is set immediately before
// every terminal SetStatus call in this package (see orchestrate.go), and
// its own lock/unlock gives the same happens-before guarantee for
// everything that ran before it (including filesystem writes like
// promote's) that a direct, synchronized Status read would.
func waitForTerminal(t *testing.T, job *Job) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job.Outcome() != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job to reach a terminal outcome")
}

func TestOrchestratorPromotesOnApproval(t *testing.T) {
	repoRoot, botsDir := newTestRepo(t)
	o := &Orchestrator{Config{
		RepoRoot: repoRoot, BotsDir: botsDir, WorkDir: t.TempDir(),
		Agent: &fakeAgent{copyFrom: realBotFixture(t), newID: "new-content-bot"},
	}}

	job, err := o.StartJob("give me post ideas", "brainstorm content ideas", nil, nil)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}

	approval := waitForApproval(t, job)
	if job.BotID() != "new-content-bot" {
		t.Errorf("BotID = %q", job.BotID())
	}
	if !job.ConformOK() {
		t.Error("expected ConformOK before the review gate")
	}
	if job.BotPreview() == nil {
		t.Fatal("expected a BotPreview before the review gate")
	}
	if err := job.Decide(approval.id, true, "test"); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	waitForTerminal(t, job)
	if job.Outcome() != OutcomePromoted {
		t.Fatalf("Outcome = %q, log = %+v", job.Outcome(), job.LogEntries())
	}
	if _, err := os.Stat(filepath.Join(botsDir, "new-content-bot", "nanobot.yaml")); err != nil {
		t.Errorf("expected the bot to be promoted into BotsDir: %v", err)
	}
	if _, err := os.Stat(job.WorktreePath()); !os.IsNotExist(err) {
		t.Errorf("expected the worktree to be cleaned up after promotion, stat err = %v", err)
	}
}

func TestOrchestratorRejectsWhenReviewerDeclines(t *testing.T) {
	repoRoot, botsDir := newTestRepo(t)
	o := &Orchestrator{Config{
		RepoRoot: repoRoot, BotsDir: botsDir, WorkDir: t.TempDir(),
		Agent: &fakeAgent{copyFrom: realBotFixture(t), newID: "new-content-bot"},
	}}

	job, err := o.StartJob("give me post ideas", "brainstorm content ideas", nil, nil)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	approval := waitForApproval(t, job)
	if err := job.Decide(approval.id, false, "test"); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	waitForTerminal(t, job)
	if job.Outcome() != OutcomeRejected {
		t.Fatalf("Outcome = %q", job.Outcome())
	}
	if _, err := os.Stat(filepath.Join(botsDir, "new-content-bot")); !os.IsNotExist(err) {
		t.Error("expected BotsDir to remain untouched on rejection")
	}
	if _, err := os.Stat(job.WorktreePath()); err != nil {
		t.Errorf("expected the worktree to be preserved on rejection, stat err = %v", err)
	}
}

func TestOrchestratorRejectsSandboxViolation(t *testing.T) {
	repoRoot, botsDir := newTestRepo(t)
	o := &Orchestrator{Config{
		RepoRoot: repoRoot, BotsDir: botsDir, WorkDir: t.TempDir(),
		Agent: &fakeAgent{copyFrom: realBotFixture(t), newID: "new-content-bot", extraPath: filepath.Join("internal", "sneaky.go")},
	}}

	job, err := o.StartJob("give me post ideas", "brainstorm content ideas", nil, nil)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	waitForTerminal(t, job)
	if job.Outcome() != OutcomeSandboxViolation {
		t.Fatalf("Outcome = %q, log = %+v", job.Outcome(), job.LogEntries())
	}
	if len(job.PendingApprovals()) != 0 {
		t.Error("a sandbox violation should never reach a human review gate")
	}
	if _, err := os.Stat(filepath.Join(botsDir, "new-content-bot")); !os.IsNotExist(err) {
		t.Error("expected BotsDir to remain untouched on a sandbox violation")
	}
}

func TestOrchestratorFailsWhenConformFails(t *testing.T) {
	repoRoot, botsDir := newTestRepo(t)
	o := &Orchestrator{Config{
		RepoRoot: repoRoot, BotsDir: botsDir, WorkDir: t.TempDir(),
		// git doesn't track empty directories, so a bare mkdir would leave
		// no git-visible change at all (failing sandbox verification, a
		// different, earlier check than the one this test targets) — seed
		// one throwaway file so the directory shows up in `git status`,
		// with no nanobot.yaml at all, so conformance fails instead.
		Agent: agentFunc(func(ctx context.Context, workDir string, in BriefInput, events chan<- Event) error {
			dir := filepath.Join(workDir, "bots", "broken-bot")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "README.md"), []byte("todo"), 0o644)
		}),
	}}

	job, err := o.StartJob("give me post ideas", "brainstorm content ideas", nil, nil)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}

	waitForTerminal(t, job)
	if job.Outcome() != OutcomeConformFailed {
		t.Fatalf("Outcome = %q, log = %+v", job.Outcome(), job.LogEntries())
	}
	if len(job.PendingApprovals()) != 0 {
		t.Error("a non-conforming bot should never reach a human review gate")
	}
}

func TestOrchestratorEnforcesMaxIterations(t *testing.T) {
	repoRoot, botsDir := newTestRepo(t)
	const maxIter = 3
	o := &Orchestrator{Config{
		RepoRoot: repoRoot, BotsDir: botsDir, WorkDir: t.TempDir(),
		MaxIterations: maxIter,
		MaxWallClock:  time.Minute, // long enough that the iteration cap, not the wall clock, is what ends this
		// A runaway agent: keeps "attempting" conform forever and never
		// finishes on its own — only cancellation (via the iteration cap)
		// ends it.
		Agent: agentFunc(func(ctx context.Context, workDir string, in BriefInput, events chan<- Event) error {
			for i := 0; i < maxIter+5; i++ {
				select {
				case events <- Event{Phase: "tool", Msg: "conform: attempt"}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			<-ctx.Done()
			return ctx.Err()
		}),
	}}

	job, err := o.StartJob("give me post ideas", "brainstorm content ideas", nil, nil)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	waitForTerminal(t, job)
	if job.Outcome() != OutcomeTimeout {
		t.Fatalf("Outcome = %q, log = %+v", job.Outcome(), job.LogEntries())
	}
	if job.Iterations() <= maxIter {
		t.Errorf("Iterations = %d, expected it to exceed maxIter=%d before stopping", job.Iterations(), maxIter)
	}
}

// agentFunc adapts a plain function to the Agent interface, for tests that
// want a one-off Agent without a named type.
type agentFunc func(ctx context.Context, workDir string, in BriefInput, events chan<- Event) error

func (f agentFunc) Run(ctx context.Context, workDir string, in BriefInput, events chan<- Event) error {
	return f(ctx, workDir, in, events)
}

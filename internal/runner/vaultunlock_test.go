package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

// v3 Phase 4 item 4: a passkey-locked 1Claw vault pauses the run in a
// distinct, visible state and retries automatically once unlocked — not a
// failure, and not counted against the bot's own retry budget, which
// TestNoRetryByDefault (waves_test.go) proves is otherwise 0 by default.
func TestAVaultLockRetriesWithoutCountingAgainstTheRetryBudget(t *testing.T) {
	var attempts int
	var sawStatuses []RunStatus
	o := &Orchestrator{
		runBotFn: func(run *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
			attempts++
			sawStatuses = append(sawStatuses, run.GetStatus())
			if attempts < 3 {
				return &oneclaw.VaultLockedError{Detail: "waiting on a passkey"}
			}
			return nil
		},
	}
	var waited int
	o.vaultUnlockWaitFn = func(_ context.Context, d time.Duration) {
		waited++
		if d != vaultUnlockPollInterval {
			t.Errorf("wait duration = %v, want %v", d, vaultUnlockPollInterval)
		}
	}
	// Retry: 0 (the default) — a vault lock must not need an opt-in retry
	// budget to eventually succeed, unlike an ordinary transient failure.
	rs := swarmWith([]schema.BotRef{{ID: "locked"}}, nil)

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"locked"}}); err != nil {
		t.Fatalf("gave up on a vault lock that later cleared: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if waited != 2 {
		t.Errorf("waited %d times, want 2 (once between each locked attempt)", waited)
	}
	// sawStatuses[0] is the very first attempt: whatever NewRun starts as
	// (this test calls runLevels directly, the same level the rest of this
	// package's retry tests use — executeSwarm is what sets StatusRunning
	// before a real run's first wave). Every attempt after a lock was hit
	// sees StatusAwaitingUnlock, set before the retry.
	if sawStatuses[0] != StatusPending {
		t.Errorf("first attempt saw status %q, want pending", sawStatuses[0])
	}
	for i, s := range sawStatuses[1:] {
		if s != StatusAwaitingUnlock {
			t.Errorf("attempt %d saw status %q, want awaiting_unlock", i+2, s)
		}
	}
	// runLevels alone (this test's level, matching how the other retry
	// tests in this package call it) never reaches StatusSucceeded — only
	// executeSwarm does, once every wave finishes. What this proves at this
	// level is that attemptThroughVaultUnlock restores StatusRunning once
	// the lock clears, rather than leaving the run stuck reporting
	// "awaiting_unlock" forever after it's no longer true.
	if run.GetStatus() != StatusRunning {
		t.Errorf("final status = %q, want running (restored after the lock cleared)", run.GetStatus())
	}
}

// The run log has to say why nothing is happening, in words a person can
// act on — "it's locked, waiting" reads very differently from a bare error.
func TestAVaultLockLogsWhatItIsWaitingFor(t *testing.T) {
	attempts := 0
	o := &Orchestrator{
		runBotFn: func(run *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
			attempts++
			if attempts < 2 {
				return &oneclaw.VaultLockedError{Detail: "waiting on a passkey"}
			}
			return nil
		},
		vaultUnlockWaitFn: func(context.Context, time.Duration) {},
	}
	rs := swarmWith([]schema.BotRef{{ID: "locked"}}, nil)
	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"locked"}}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range run.LogEntries() {
		if l.Bot == "locked" && strings.Contains(l.Msg, "waiting on a passkey") && strings.Contains(l.Msg, "will retry automatically") {
			found = true
		}
	}
	if !found {
		t.Error("no log line explained the vault lock and that it will retry automatically")
	}
}

// A user stopping the run while it's waiting on a lock must end the wait,
// not keep polling forever after nobody wants the answer anymore.
func TestStoppingARunEndsAVaultUnlockWaitEarly(t *testing.T) {
	var attempts int
	var run *Run
	o := &Orchestrator{
		runBotFn: func(r *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
			attempts++
			return &oneclaw.VaultLockedError{Detail: "still locked"}
		},
	}
	o.vaultUnlockWaitFn = func(context.Context, time.Duration) {
		// Simulate the user stopping the run mid-wait, exactly once, so
		// the loop above sees it on the next check rather than spinning.
		if attempts == 1 {
			run.Stop()
		}
	}
	rs := swarmWith([]schema.BotRef{{ID: "locked"}}, nil)
	run = NewRun("probe")
	err := o.runLevels(run, rs, [][]string{{"locked"}})
	if err == nil {
		t.Fatal("expected the stopped run to surface an error rather than succeed")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want exactly 1 — stopping must end the wait, not let another attempt through", attempts)
	}
}

// The exact hazard docs/error-policy.md's CheckRetry already refuses at
// plan time for an ordinary retry: — a retry re-runs the whole bot, and a
// bot that writes could have already sent something before a later step's
// vault read hit the lock. CheckRetry can't catch this one (there's no
// retry: written down for a vault lock), so the same guard has to live in
// the runner instead: a locked vault on a writing bot fails once, honestly,
// rather than silently risking a duplicate send.
func TestAVaultLockOnAWritingBotIsNotRetried(t *testing.T) {
	var attempts int
	o := &Orchestrator{
		runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot) error {
			attempts++
			return &oneclaw.VaultLockedError{Detail: "still locked"}
		},
	}
	o.vaultUnlockWaitFn = func(context.Context, time.Duration) {
		t.Fatal("a writing bot's vault lock must not wait at all — it must fail immediately")
	}
	ref := schema.BotRef{ID: "sender"}
	rs := &planner.ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{Bots: []schema.BotRef{ref}}},
		Bots: map[string]*planner.ResolvedBot{
			"sender": {Ref: ref, Nanobot: &schema.Nanobot{
				Metadata: schema.Metadata{Name: "sender", Version: "0.1.0"},
				Spec: schema.NanobotSpec{Guardrails: schema.Guardrails{
					WritesAllowed: []string{"gmail.send"},
				}},
			}},
		},
	}

	run := NewRun("probe")
	err := o.runLevels(run, rs, [][]string{{"sender"}})
	if err == nil {
		t.Fatal("expected the vault lock to fail the run, not succeed silently")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want exactly 1 — a writing bot's vault lock must never retry", attempts)
	}
	if run.GetStatus() == StatusAwaitingUnlock {
		t.Error("status = awaiting_unlock, want a plain failure — this bot never actually waits")
	}
}

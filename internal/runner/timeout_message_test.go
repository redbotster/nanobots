package runner

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Every one of the 54 runs on this machine that hit the 30-minute container
// cap was sitting on an unanswered approval. All of them reported "container
// exceeded 30m0s and was stopped", which sends you looking for a hung bot
// instead of the decision you missed.
func TestATimeoutWaitingOnAnApprovalSaysSo(t *testing.T) {
	run := NewRun("daily-email-recap")
	go func() {
		// RequestApproval blocks until decided; this one never is, which is
		// the whole scenario.
		_, _, _ = run.RequestApproval("mailer", "approve", "Send 'recap.pdf' to me@example.com?", "high", time.Hour)
	}()
	waitForApproval(t, run)

	got := describeTimeout(run, "mailer", 30*time.Minute,
		errors.New("container exceeded 30m0s and was stopped"))

	msg := got.Error()
	for _, want := range []string{
		"nobody answered the approval",
		"Send 'recap.pdf' to me@example.com?",
		"approve it from the Runs page",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message is missing %q:\n  %s", want, msg)
		}
	}
	if strings.Contains(msg, "container exceeded") {
		t.Errorf("still leads with the container timeout:\n  %s", msg)
	}
}

// A bot that genuinely hangs with nothing pending must still report the
// timeout it actually hit — the point is to stop mislabelling one case, not
// to relabel every timeout as somebody's fault.
func TestATimeoutWithNoApprovalPendingIsLeftAlone(t *testing.T) {
	run := NewRun("content-engine")
	original := errors.New("container exceeded 1m0s and was stopped")

	if got := describeTimeout(run, "writer", time.Minute, original); got != original {
		t.Errorf("rewrote a plain timeout: %v", got)
	}
}

// An approval pending on a *different* bot in the same run is not this
// bot's excuse. A swarm often has one bot waiting on approval while another
// genuinely hangs, and blaming the wrong one is its own honesty bug.
func TestAnotherBotsApprovalIsNotThisBotsExcuse(t *testing.T) {
	run := NewRun("get-paid")
	go func() { _, _, _ = run.RequestApproval("mailer", "send", "Send 3 reminders?", "high", time.Hour) }()
	waitForApproval(t, run)

	original := errors.New("container exceeded 30m0s and was stopped")
	if got := describeTimeout(run, "chart-maker", 30*time.Minute, original); got != original {
		t.Errorf("blamed chart-maker's timeout on mailer's approval: %v", got)
	}
}

// Non-timeout failures pass through untouched, pending approval or not.
func TestANonTimeoutFailureIsNeverRelabelled(t *testing.T) {
	run := NewRun("support-desk-lite")
	go func() { _, _, _ = run.RequestApproval("notify", "post", "Post to #support?", "medium", time.Hour) }()
	waitForApproval(t, run)

	original := errors.New("container exited 1: nanobot-agent: error: bot notify failed")
	if got := describeTimeout(run, "notify", 30*time.Minute, original); got != original {
		t.Errorf("rewrote a crash as an approval timeout: %v", got)
	}
}

func waitForApproval(t *testing.T, run *Run) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(run.PendingApprovals()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no approval was ever registered")
}

// The approval gate's own timeout, for when it does get to fire. It used to
// name the approval's UUID, which is the one thing the person reading the
// Runs page cannot act on: it is gone from the queue by then and they have
// nowhere to look it up.
func TestAnExpiredApprovalNamesWhatWentUnanswered(t *testing.T) {
	run := NewRun("weekly-client-report")

	_, _, err := run.RequestApproval("mailer", "send",
		"Email 'March invoice.pdf' to finance@acme.com?", "high", 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected the approval to time out")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Email 'March invoice.pdf' to finance@acme.com?") {
		t.Errorf("does not say what went unanswered:\n  %s", msg)
	}
	if strings.Contains(msg, run.ID) {
		t.Errorf("still leads with an id nobody can use:\n  %s", msg)
	}
}

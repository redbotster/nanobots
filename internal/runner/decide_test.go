package runner

import (
	"strings"
	"testing"
	"time"
)

// A question you have answered has to stop being asked.
//
// The waiting goroutine is what removes an approval from the run's map, so
// there is a window after Decide where the approval is still in there. The
// CLI re-checks PendingApprovals on every log line, and one line arriving
// in that window was enough to prompt again, hit EOF on a drained stdin,
// and print "declined — nobody — no terminal attached to ask" under a run
// that had just been approved.
//
// Two separate faults, one cause:
//   - the run said something untrue about itself;
//   - the second Decide set DeclinedByUser on an approved run, and would
//     have blocked on the run's own mutex — holding it — if the waiter had
//     not already drained the one-slot channel.
func TestAnAnsweredApprovalIsNoLongerPending(t *testing.T) {
	run := NewRun("probe")
	approved := make(chan bool, 1)
	go func() {
		ok, _, _ := run.RequestApproval("sender", "gate", "Send it?", "high", 5*time.Second)
		approved <- ok
	}()

	pending := waitForPending(t, run)
	if err := run.Decide(pending, true, "cli"); err != nil {
		t.Fatalf("first decision: %v", err)
	}

	// Immediately, not once the waiter happens to wake up.
	if got := run.PendingApprovals(); len(got) != 0 {
		t.Errorf("an answered approval is still listed as pending: %d", len(got))
	}

	// A second answer is refused rather than overwriting the first.
	err := run.Decide(pending, false, "nobody — no terminal attached to ask")
	if err == nil {
		t.Fatal("a second decision was accepted")
	}
	if !strings.Contains(err.Error(), "already answered") {
		t.Errorf("err = %v, want it to say the question was already answered", err)
	}

	select {
	case ok := <-approved:
		if !ok {
			t.Error("the run was declined by a decision nobody made")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the gate never returned")
	}
	if run.WasDeclinedByUser() {
		t.Error("an approved run recorded that a human declined it")
	}
}

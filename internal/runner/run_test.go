package runner

import (
	"errors"
	"github.com/redbotster/nanobots/internal/step"
	"sync"
	"testing"
	"time"
)

func TestRunAllOutputsReturnsEveryBot(t *testing.T) {
	run := NewRun("s")
	run.SetBotOutputs("recap", map[string]any{"drive_file_id": "f-1"})
	run.SetBotOutputs("mailer", map[string]any{"message_id": "m-1"})

	all := run.AllOutputs()
	if len(all) != 2 {
		t.Fatalf("AllOutputs() has %d bots, want 2", len(all))
	}
	if all["recap"]["drive_file_id"] != "f-1" {
		t.Errorf("recap outputs = %#v", all["recap"])
	}
	if all["mailer"]["message_id"] != "m-1" {
		t.Errorf("mailer outputs = %#v", all["mailer"])
	}
}

func TestRunApprovalLifecycle(t *testing.T) {
	run := NewRun("s")
	done := make(chan struct{})
	var approved bool
	var decidedBy string
	go func() {
		approved, decidedBy, _ = run.RequestApproval("bot", "gate", "send it?", "low", 5*time.Second)
		close(done)
	}()

	// Wait for the approval to actually register before deciding it.
	deadline := time.Now().Add(2 * time.Second)
	var pending []*PendingApproval
	for time.Now().Before(deadline) {
		if pending = run.PendingApprovals(); len(pending) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(pending))
	}
	if err := run.Decide(pending[0].ID, true, "kevin"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	<-done
	if !approved || decidedBy != "kevin" {
		t.Errorf("approved=%v decidedBy=%q, want true, kevin", approved, decidedBy)
	}
}

// Regression: Log used to snapshot subscribers, release the mutex, then
// send — racing with Unsubscribe's close() and panicking with "send on
// closed channel". Because ExecuteSwarm runs swarms in a detached
// goroutine, that panic was not caught by net/http's per-connection
// recover: closing a browser tab on a running swarm could take down the
// whole daemon.
//
// Run with -race to catch the data race, and without it to catch the panic;
// this hammers both windows.
func TestLogAndUnsubscribeDoNotRace(t *testing.T) {
	run := NewRun("swarm")
	var wg sync.WaitGroup

	// Writers, as the orchestrator's goroutine would.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				run.Log("bot", "step", "line %d", j)
			}
		}()
	}

	// Readers subscribing and disconnecting, as SSE clients do.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ch := run.Subscribe()
				select {
				case <-ch:
				default:
				}
				run.Unsubscribe(ch)
			}
		}()
	}

	wg.Wait() // a panic in any goroutine fails the test by crashing it
}

// Unsubscribing twice (a retry, a defer plus an explicit call) must not
// panic on a double close.
func TestUnsubscribeIsIdempotent(t *testing.T) {
	run := NewRun("swarm")
	ch := run.Subscribe()
	run.Unsubscribe(ch)
	run.Unsubscribe(ch)
}

// A container that hits its own max_runtime while a human is still deciding
// leaves the run failed and already written to history. A decision arriving
// after that used to flip it back to "running" — leaving a run with
// finished_at set, error set, status running, and nothing alive to ever move
// it again. On-disk history said failed; the API said running, forever.
func TestLateApprovalDoesNotResurrectAFailedRun(t *testing.T) {
	run := NewRun("post-publisher")

	decided := make(chan struct{})
	go func() {
		defer close(decided)
		approved, _, err := run.RequestApproval("publisher", "gate", "Publish this post?", "high", time.Minute)
		if err == nil {
			t.Errorf("expected an error once the run ended, got approved=%v", approved)
		}
		if approved {
			t.Error("an approval decided after the run failed must not count as approved")
		}
	}()

	// Wait for the approval to actually be pending before ending the run.
	waitFor(t, func() bool { return len(run.PendingApprovals()) == 1 })

	run.SetError(errors.New("container exceeded 60s and was stopped"))
	run.SetStatus(StatusFailed)

	select {
	case <-decided:
	case <-time.After(5 * time.Second):
		t.Fatal("RequestApproval never returned; the waiter is stuck until its full timeout")
	}

	if got := run.GetStatus(); got != StatusFailed {
		t.Errorf("status = %q, want it to stay failed", got)
	}
	if n := len(run.PendingApprovals()); n != 0 {
		t.Errorf("%d approvals still listed on a dead run — the UI would keep asking", n)
	}
	// And a human clicking Approve now gets told, not silently ignored.
	if err := run.Decide("whatever", true, "you"); err == nil {
		t.Error("deciding on an ended run should report that it ended")
	}
}

// The container's HTTP client has to outlast the longest thing a callback
// can legitimately block on. It was 5 minutes against a 30-minute approval
// budget, so approving at minute six failed the run. These two constants
// live in different packages and drifted silently; this is what notices.
func TestContainerHTTPTimeoutOutlastsApprovals(t *testing.T) {
	deps := step.NewRemoteDeps("http://host.docker.internal:7474", "token", nil)
	if deps.HTTPClient.Timeout <= ApprovalTimeout {
		t.Errorf("RemoteDeps HTTP timeout is %s but approvals may take %s — a human approving inside the budget would still fail the run",
			deps.HTTPClient.Timeout, ApprovalTimeout)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

// Three runs end `failed` and only one of them is a fault. The word a person
// reads has to tell them apart, and it used to be the raw status everywhere
// except the WebUI's run list.
func TestOutcomeNamesWhatActuallyHappened(t *testing.T) {
	for name, tc := range map[string]struct {
		build func(*Run)
		want  string
	}{
		"a run that broke": {
			build: func(r *Run) { r.SetStatus(StatusFailed) },
			want:  "failed",
		},
		"a run someone stopped": {
			build: func(r *Run) { r.StoppedByUser = true; r.SetStatus(StatusFailed) },
			want:  "stopped",
		},
		"a run someone declined": {
			build: func(r *Run) { r.DeclinedByUser = true; r.SetStatus(StatusFailed) },
			want:  "declined",
		},
		"a watch that found nothing": {
			build: func(r *Run) { r.NothingToDo = "no new file"; r.SetStatus(StatusSucceeded) },
			want:  "nothing to do",
		},
		"an ordinary success": {
			build: func(r *Run) { r.SetStatus(StatusSucceeded) },
			want:  "succeeded",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRun("probe")
			tc.build(r)
			if got := r.Outcome(); got != tc.want {
				t.Errorf("Outcome() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Stopping a run while an approval is open sets both. Stopped wins: ending
// the run is the act that decided it, and the approval never got answered.
func TestAStoppedRunReadsAsStoppedEvenIfSomethingWasDeclined(t *testing.T) {
	r := NewRun("probe")
	r.StoppedByUser = true
	r.DeclinedByUser = true
	r.SetStatus(StatusFailed)
	if got := r.Outcome(); got != "stopped" {
		t.Errorf("Outcome() = %q, want stopped", got)
	}
}

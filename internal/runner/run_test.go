package runner

import (
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

package runner

import (
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

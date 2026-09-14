package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Stop has to end a run that is parked at an approval gate. Cancelling the
// context does not reach one — it is waiting on a person, not on a context —
// so this is the case most likely to hang forever.
func TestStopReleasesARunWaitingOnAnApproval(t *testing.T) {
	run := NewRun("get-paid")
	run.SetStatus(StatusRunning)

	done := make(chan error, 1)
	go func() {
		_, _, err := run.RequestApproval("sender", "gate", "send 12 reminders", "medium", time.Minute)
		done <- err
	}()

	// Wait for the gate to actually open before stopping it.
	deadline := time.After(2 * time.Second)
	for len(run.PendingApprovals()) == 0 {
		select {
		case <-deadline:
			t.Fatal("the approval never opened")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if !run.Stop() {
		t.Fatal("Stop reported nothing to stop on a running run")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("the gate returned approved after the run was stopped")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stopping left the approval gate blocked — this is the hang Stop exists to prevent")
	}
	if !run.WasStoppedByUser() {
		t.Error("the run does not know it was stopped on purpose")
	}
}

// The run's context is what containers block on, so Stop must cancel it.
func TestStopCancelsTheRunContext(t *testing.T) {
	run := NewRun("probe")
	run.SetStatus(StatusRunning)
	if run.Context().Err() != nil {
		t.Fatal("a fresh run's context is already done")
	}
	run.Stop()
	if !errors.Is(run.Context().Err(), context.Canceled) {
		t.Errorf("context err = %v, want Canceled", run.Context().Err())
	}
}

// Stopping something already over is a double-click, not an error — but the
// caller is told nothing happened so it can say so.
func TestStopOnAFinishedRunReportsNothingToStop(t *testing.T) {
	run := NewRun("probe")
	run.SetStatus(StatusSucceeded)
	if run.Stop() {
		t.Error("Stop claimed to stop a run that had already succeeded")
	}
	if run.WasStoppedByUser() {
		t.Error("a finished run was marked as stopped by the user")
	}
}

// A Run built as a bare struct literal has no context. Handing back nil
// would panic deep inside exec, a long way from the line that forgot the
// constructor.
func TestContextIsNeverNil(t *testing.T) {
	if (&Run{}).Context() == nil {
		t.Fatal("Context() returned nil")
	}
}

package runner

import (
	"errors"
	"testing"
	"time"
)

// The runs-list SSE stream (internal/api.handleRunsEvents) subscribes here
// instead of polling GET /api/runs. A subscriber has to hear about a run
// the moment it exists, not wait for its first status change — a fresh
// tab's snapshot already covers existing runs, but a run added between the
// snapshot and the subscribe would otherwise never be seen at all.
func TestAddingARunBroadcastsItsID(t *testing.T) {
	s := NewRunStore()
	ch := s.SubscribeChanges()
	defer s.UnsubscribeChanges(ch)

	run := NewRun("probe")
	s.Add(run)

	select {
	case id := <-ch:
		if id != run.ID {
			t.Errorf("broadcast id = %q, want %q", id, run.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("adding a run never broadcast its id")
	}
}

// Every mutation the runs-list summary actually renders has to reach a
// subscriber: status, error, stopping, and nothing-to-do. Missing one of
// these would mean the SSE-driven list disagreeing with what GET
// /api/runs/{id} already says about the same run.
func TestEverySummaryFieldChangeBroadcasts(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(*Run)
	}{
		{"status", func(r *Run) { r.SetStatus(StatusRunning) }},
		{"error", func(r *Run) { r.SetError(errors.New("boom")) }},
		{"nothing to do", func(r *Run) { r.SetNothingToDo("no new file") }},
		{"stop", func(r *Run) { r.Stop() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewRunStore()
			run := NewRun("probe")

			// Subscribed before Add: Add's own broadcast fires the moment
			// it's called, and a subscriber that joined after would never
			// see it — the same "subscribe before snapshotting" ordering
			// handleRunsEvents itself depends on.
			ch := s.SubscribeChanges()
			defer s.UnsubscribeChanges(ch)
			s.Add(run)
			drainOne(t, ch, "Add") // Add's own broadcast, not what's under test

			tc.do(run)

			select {
			case id := <-ch:
				if id != run.ID {
					t.Errorf("broadcast id = %q, want %q", id, run.ID)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s did not broadcast a change", tc.name)
			}
		})
	}
}

// A declined approval has to be visible in the list the instant the
// decision is recorded — not two changes later, once the bot's own
// goroutine wakes up and calls SetStatus(Running).
func TestDecidingAnApprovalBroadcasts(t *testing.T) {
	s := NewRunStore()
	run := NewRun("probe")
	opened := make(chan string, 1)

	ch := s.SubscribeChanges()
	defer s.UnsubscribeChanges(ch)
	s.Add(run)
	drainOne(t, ch, "Add")

	go func() {
		_, _, _ = run.RequestApproval("sender", "approve", "send it", "medium", time.Second, func(id string) {
			opened <- id
		})
	}()

	var approvalID string
	select {
	case approvalID = <-opened:
	case <-time.After(time.Second):
		t.Fatal("the approval never opened")
	}
	drainOne(t, ch, "SetStatus(StatusAwaitingApproval)")

	if err := run.Decide(approvalID, false, "cli"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-ch:
		if id != run.ID {
			t.Errorf("broadcast id = %q, want %q", id, run.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("deciding an approval did not broadcast")
	}
}

// drainOne consumes exactly one broadcast, failing (rather than hanging the
// whole test binary) if none arrives — the first version of these tests
// used a bare `<-ch` here and blocked for the full 10-minute test timeout
// when the drain target had already been lost (broadcast before the
// subscription existed), which read as a mysterious hang far from its
// actual cause.
func drainOne(t *testing.T, ch chan string, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("expected a broadcast for %s, got none", what)
	}
}

// An unsubscribed channel must stop receiving — a leaked SSE connection
// (a client that closed its tab) is a goroutine and a channel forever, if
// nothing ever calls UnsubscribeChanges for it.
func TestUnsubscribingStopsDelivery(t *testing.T) {
	s := NewRunStore()
	ch := s.SubscribeChanges()
	s.UnsubscribeChanges(ch)

	s.Add(NewRun("probe"))

	select {
	case id, ok := <-ch:
		if ok {
			t.Errorf("received %q on an unsubscribed channel", id)
		}
		// A closed channel reading the zero value is the expected shape.
	case <-time.After(50 * time.Millisecond):
		t.Error("the channel was neither closed nor sent to — Unsubscribe should have closed it")
	}
}

package runner

import (
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

// The bug this whole item fixes, as a test.
//
// `POST /v1/approvals/request` is agent-only. Sent with the human key it
// answers 403 "Only agents can request approvals." — so a mirror built on
// the human client can never open anything, which is exactly what shipped
// and stayed unnoticed: the old fake accepted any bearer token, and on the
// real API the call was never reached at all because no approving bot had
// an agent.
//
// If someone wires the human client back in, this fails.
func TestTheMirrorIsRefusedWithoutAnAgentCredential(t *testing.T) {
	orig := mirrorPoll
	mirrorPoll = 10 * time.Millisecond
	t.Cleanup(func() { mirrorPoll = orig })

	// Points DefaultBaseURL at the fake and hands back an agent client;
	// this test deliberately throws that away and uses a human one.
	fakeApprovalAPI(t, func(int) string { return "approved" })
	human := oneclaw.NewClient("1ck_human")

	run := NewRun("probe")
	a := &RunQueueApprover{
		Run: run, Bot: "sender", Step: "approve",
		Mirror: func() (*oneclaw.Client, string, error) { return human, "agent-1", nil },
	}

	done := make(chan bool, 1)
	go func() {
		ok, _, _ := a.Approve("Send it", "high")
		done <- ok
	}()

	// The local gate still opens, and is still the one that decides.
	id := waitForPending(t, run)
	if said := logContains(run, "could not also ask on 1Claw"); !said {
		t.Error("a refused mirror said nothing in the run log")
	}
	if said := logContains(run, "also asked on 1Claw"); said {
		t.Error("the run claimed 1Claw was asked, and it was refused")
	}
	_ = run.Decide(id, true, "cli")
	select {
	case ok := <-done:
		if !ok {
			t.Error("the local decision did not take")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the local decision never arrived")
	}
}

// No 1Claw at all is the common case — a fresh install, a demo run — and it
// must be silent rather than logging a failure per gate.
func TestNoMirrorIsSilent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mirror ApprovalMirror
	}{
		{"no mirror at all", nil},
		{"a mirror with nothing behind it", func() (*oneclaw.Client, string, error) {
			return nil, "", nil
		}},
		{"1Claw configured but no agent resolved", func() (*oneclaw.Client, string, error) {
			return oneclaw.NewAgentClient("ocv_k"), "", nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := NewRun("probe")
			a := &RunQueueApprover{Run: run, Bot: "sender", Step: "approve", Mirror: tc.mirror}
			go func() { _, _, _ = a.Approve("Send it", "low") }()

			id := waitForPending(t, run)
			for _, entry := range run.LogEntries() {
				if strings.Contains(entry.Msg, "1Claw") {
					t.Errorf("a run without 1Claw mentioned it: %q", entry.Msg)
				}
			}
			_ = run.Decide(id, true, "cli")
		})
	}
}

// Resolving the agent can fail for a reason worth reading — most likely an
// agent named nanobots exists on 1Claw whose key was shown once and is not
// saved here. That has to reach the log, not be swallowed.
func TestAMirrorThatCannotResolveItsAgentSaysSo(t *testing.T) {
	run := NewRun("probe")
	a := &RunQueueApprover{
		Run: run, Bot: "sender", Step: "approve",
		Mirror: func() (*oneclaw.Client, string, error) {
			return nil, "", errAgentUnavailable
		},
	}
	go func() { _, _, _ = a.Approve("Send it", "low") }()

	id := waitForPending(t, run)
	if !logContains(run, "could not also ask on 1Claw") {
		t.Error("an unresolvable agent said nothing in the run log")
	}
	if !logContains(run, errAgentUnavailable.Error()) {
		t.Error("the log did not carry the reason")
	}
	_ = run.Decide(id, true, "cli")
}

var errAgentUnavailable = errTest("1Claw approvals agent: its api_key was only ever shown once")

type errTest string

func (e errTest) Error() string { return string(e) }

func waitForPending(t *testing.T, run *Run) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if p := run.PendingApprovals(); len(p) > 0 {
			return p[0].ID
		}
		select {
		case <-deadline:
			t.Fatal("no local approval was ever queued")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func logContains(run *Run, want string) bool {
	for _, entry := range run.LogEntries() {
		if strings.Contains(entry.Msg, want) {
			return true
		}
	}
	return false
}

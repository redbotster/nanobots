package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	// Waited for, not sampled once: the mirror runs from the callback that
	// fires as the gate opens, and it has an HTTP round trip to make first.
	// Asserting immediately passed on a fast laptop and failed in CI, which
	// is the only reason this comment exists.
	waitForLog(t, run, "could not also ask on 1Claw")
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
			done := make(chan struct{})
			go func() { _, _, _ = a.Approve("Send it", "low"); close(done) }()

			_ = run.Decide(waitForPending(t, run), true, "cli")
			// Asserted after the gate has closed, not while it is open: a
			// check that runs before the mirror would have spoken proves
			// nothing about whether it stays quiet.
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the gate never returned")
			}
			for _, entry := range run.LogEntries() {
				if strings.Contains(entry.Msg, "1Claw") {
					t.Errorf("a run without 1Claw mentioned it: %q", entry.Msg)
				}
			}
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
	waitForLog(t, run, "could not also ask on 1Claw")
	if !logContains(run, errAgentUnavailable.Error()) {
		t.Error("the log did not carry the reason")
	}
	_ = run.Decide(id, true, "cli")
}

// docs/1claw-feature-requests.md #3: cancelling a 1Claw approval didn't
// exist when the mirror was written, so a question answered locally left
// its remote copy to expire on its own thirty minutes later — a stale
// question sitting in a real person's queue for the rest of that timeout.
// This is the fix, as a test.
func TestAnsweringLocallyCancelsTheMirror(t *testing.T) {
	orig := mirrorPoll
	mirrorPoll = 10 * time.Millisecond
	t.Cleanup(func() { mirrorPoll = orig })

	var mu sync.Mutex
	var canceled string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/agent-token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "agent-tok", "token_type": "Bearer", "expires_in": 86400,
			})
		case strings.HasSuffix(r.URL.Path, "/approvals/request"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "remote-1", "status": "pending"})
		case strings.HasSuffix(r.URL.Path, "/approvals/remote-1/status"):
			// Never resolves on its own — the local decision has to be
			// what ends this, same as a real person answering elsewhere.
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
		case strings.HasSuffix(r.URL.Path, "/approvals/remote-1/cancel"):
			mu.Lock()
			canceled = "remote-1"
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	origBaseURL := oneclaw.DefaultBaseURL
	oneclaw.DefaultBaseURL = srv.URL
	t.Cleanup(func() { oneclaw.DefaultBaseURL = origBaseURL })
	client := oneclaw.NewAgentClient("ocv_test-key")

	run := NewRun("probe")
	a := &RunQueueApprover{
		Run: run, Bot: "sender", Step: "approve",
		Mirror: func() (*oneclaw.Client, string, error) { return client, "agent-1", nil },
	}
	done := make(chan bool, 1)
	go func() {
		ok, _, _ := a.Approve("Send it", "high")
		done <- ok
	}()

	id := waitForPending(t, run)
	waitForLog(t, run, "also asked on 1Claw")
	_ = run.Decide(id, true, "cli")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the local decision never arrived")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := canceled
		mu.Unlock()
		if got == "remote-1" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("answering locally never cancelled the 1Claw mirror")
}

// A cancel that fails must not be fatal to anything — the local answer
// already stands, and this is best-effort tidying of a queue on another
// service. It has to reach the log, though, same as every other failure in
// this function.
func TestACancelThatFailsIsLoggedNotFatal(t *testing.T) {
	orig := mirrorPoll
	mirrorPoll = 10 * time.Millisecond
	t.Cleanup(func() { mirrorPoll = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/agent-token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "agent-tok", "token_type": "Bearer", "expires_in": 86400,
			})
		case strings.HasSuffix(r.URL.Path, "/approvals/request"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "remote-1", "status": "pending"})
		case strings.HasSuffix(r.URL.Path, "/approvals/remote-1/status"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
		case strings.HasSuffix(r.URL.Path, "/approvals/remote-1/cancel"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	origBaseURL := oneclaw.DefaultBaseURL
	oneclaw.DefaultBaseURL = srv.URL
	t.Cleanup(func() { oneclaw.DefaultBaseURL = origBaseURL })
	client := oneclaw.NewAgentClient("ocv_test-key")

	run := NewRun("probe")
	a := &RunQueueApprover{
		Run: run, Bot: "sender", Step: "approve",
		Mirror: func() (*oneclaw.Client, string, error) { return client, "agent-1", nil },
	}
	done := make(chan bool, 1)
	go func() {
		ok, _, _ := a.Approve("Send it", "high")
		done <- ok
	}()

	id := waitForPending(t, run)
	waitForLog(t, run, "also asked on 1Claw")
	_ = run.Decide(id, true, "cli")

	select {
	case ok := <-done:
		if !ok {
			t.Error("a failed remote cancel must not affect the local decision")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the local decision never arrived")
	}
	waitForLog(t, run, "could not withdraw the 1Claw copy")
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

func waitForLog(t *testing.T, run *Run, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if logContains(run, want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("the run log never said %q", want)
}

func logContains(run *Run, want string) bool {
	for _, entry := range run.LogEntries() {
		if strings.Contains(entry.Msg, want) {
			return true
		}
	}
	return false
}

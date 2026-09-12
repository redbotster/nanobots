package api

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// remoteThrough wires a real RemoteDeps — the thing that runs inside a bot
// container — to a real Server over a real HTTP listener, with hostDeps
// standing in for whatever the daemon resolved for that run. Every
// assertion below therefore crosses the same boundary a containerised run
// crosses, rather than calling the handler directly.
func remoteThrough(t *testing.T, hostDeps step.Deps) *step.RemoteDeps {
	t.Helper()
	reg := runner.NewCallbackRegistry()
	reg.Register("tok", hostDeps)
	srv := httptest.NewServer((&Server{Callbacks: reg}).Handler())
	t.Cleanup(srv.Close)
	return step.NewRemoteDeps(srv.URL, "tok", nil)
}

// demoDepsWithRecall returns DemoDeps over a fixtures dir containing (or
// not containing) memory.recall.json. Using the real DemoDeps rather than a
// hand-written fake is deliberate: a fake would be free to return whatever
// makes the test pass, and the last recall bug in this repo was exactly a
// fake returning a lookalike error.
func demoDepsWithRecall(t *testing.T, fixture string) *step.DemoDeps {
	t.Helper()
	dir := t.TempDir()
	if fixture != "" {
		if err := os.WriteFile(filepath.Join(dir, "memory.recall.json"), []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return step.NewDemoDeps(dir, nil)
}

// "This deployment has no recall" is a fact, not a failure, and the bot
// needs to tell it apart from "recall broke" in order to honour
// `optional: true`. It cannot: an error string can't be type-asserted after
// crossing HTTP. So the host flattens the sentinel to supported:false and
// the container rebuilds it.
//
// Nothing tested that until now. Both halves were verified by running a
// container once, by hand — and every in-process test would have stayed
// green if the flattening broke, while every real run of inbox-triage,
// support-triage and draft-replies failed outright instead of degrading.
func TestRecallSentinelSurvivesTheCallbackBoundary(t *testing.T) {
	remote := remoteThrough(t, demoDepsWithRecall(t, ""))

	answer, err := remote.MemoryRecall("inbox-triage", "what is urgent?")
	if !errors.Is(err, memory.ErrNoRecall) {
		t.Fatalf("err = %v, want ErrNoRecall to survive the round trip", err)
	}
	if answer != "" {
		t.Errorf("answer = %q, want empty alongside the sentinel", answer)
	}
}

func TestRecallAnswerSurvivesTheCallbackBoundary(t *testing.T) {
	remote := remoteThrough(t, demoDepsWithRecall(t,
		`{"what is urgent?":"Billing, always. Newsletters, never."}`))

	answer, err := remote.MemoryRecall("inbox-triage", "what is urgent?")
	if err != nil {
		t.Fatalf("MemoryRecall: %v", err)
	}
	if answer != "Billing, always. Newsletters, never." {
		t.Errorf("answer = %q", answer)
	}
}

// A recall-capable backend that knows nothing yet answers empty, and that
// must NOT come back as the no-recall sentinel — one means "ask me again
// next week", the other means "reconfigure your deployment", and a bot with
// a required recall step should keep running on the first and fail on the
// second.
func TestEmptyRecallAnswerIsNotTheNoRecallSentinel(t *testing.T) {
	remote := remoteThrough(t, demoDepsWithRecall(t, `{"something else":"..."}`))

	answer, err := remote.MemoryRecall("inbox-triage", "what is urgent?")
	if err != nil {
		t.Fatalf("nothing known yet is not an error: %v", err)
	}
	if answer != "" {
		t.Errorf("answer = %q, want empty", answer)
	}
}

// memory.remember must not fail a run, including when the round trip
// reaches a backend with nowhere to put it.
func TestRememberCrossesTheBoundaryWithoutFailing(t *testing.T) {
	remote := remoteThrough(t, demoDepsWithRecall(t, ""))
	if err := remote.MemoryRemember("inbox-triage", "triaged 3 as urgent"); err != nil {
		t.Errorf("MemoryRemember: %v", err)
	}
}

// An unregistered token is the shape a stale or forged container has. It
// must be rejected, and the rejection must reach the caller as an error
// rather than as an empty answer that a bot would treat as "nothing known".
func TestARejectedTokenIsNotMistakenForNoRecall(t *testing.T) {
	reg := runner.NewCallbackRegistry()
	reg.Register("tok", demoDepsWithRecall(t, `"anything"`))
	srv := httptest.NewServer((&Server{Callbacks: reg}).Handler())
	defer srv.Close()

	remote := step.NewRemoteDeps(srv.URL, "not-the-token", nil)
	if _, err := remote.MemoryRecall("inbox-triage", "?"); err == nil {
		t.Error("an unknown run token was accepted")
	} else if errors.Is(err, memory.ErrNoRecall) {
		t.Errorf("a rejected token was reported as 'no recall configured': %v", err)
	}
}

// `found` is how a bot tells "nothing stored yet" from "stored, and it was
// empty". competitor-watch decides whether everything is new on exactly
// that bit, so losing it across the wire would make the bot report every
// competitor as newly changed on every run — which is the failure the
// local memory backend was added to end.
func TestMemoryGetFoundFlagSurvivesTheCallbackBoundary(t *testing.T) {
	host := demoDepsWithRecall(t, "")
	remote := remoteThrough(t, host)

	if _, found, err := remote.MemoryGet("competitor-watch", "last_summary"); err != nil || found {
		t.Fatalf("nothing stored yet: found=%v err=%v", found, err)
	}
	if err := remote.MemoryPut("competitor-watch", "last_summary", ""); err != nil {
		t.Fatal(err)
	}
	value, found, err := remote.MemoryGet("competitor-watch", "last_summary")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("an empty stored value came back as nothing stored")
	}
	if value != "" {
		t.Errorf("value = %q", value)
	}
}

// A rejected approval is a decision the swarm acts on, not a failure. If it
// crossed as an error, every standalone `approve` brick would abort its run
// on "no" instead of routing the "no" onward.
func TestApprovalRejectionCrossesAsADecisionNotAnError(t *testing.T) {
	host := demoDepsWithRecall(t, "")
	host.Approver = rejectingApprover{}
	remote := remoteThrough(t, host)

	approved, decidedBy, err := remote.Approve("pay $4,000", "high")
	if err != nil {
		t.Fatalf("a rejection came back as an error: %v", err)
	}
	if approved {
		t.Error("approved = true, want false")
	}
	if decidedBy != "the-reviewer" {
		t.Errorf("decided_by = %q — who decided is lost", decidedBy)
	}
}

type rejectingApprover struct{}

func (rejectingApprover) Approve(string, string) (bool, string, error) {
	return false, "the-reviewer", nil
}

// A host-side failure must arrive as an error, not as a zero value that a
// bot would take at face value — service.call with no fixture is the
// cheapest way to produce one.
func TestHostSideFailureCrossesAsAnError(t *testing.T) {
	remote := remoteThrough(t, demoDepsWithRecall(t, ""))

	out, err := remote.ServiceCall(schema.Service{ID: "gmail"}, "messages.list", nil)
	if err == nil {
		t.Fatalf("a missing fixture came back as success: %#v", out)
	}
	if !strings.Contains(err.Error(), "fixture") {
		t.Errorf("err = %v — the host's own explanation didn't survive", err)
	}
}

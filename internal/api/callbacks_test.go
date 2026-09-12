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

// recordingDeps notes what the host side was actually asked, so a test can
// check that the arguments a bot sent are the arguments the daemon
// received. It only records — every semantic decision in these tests is
// still made by the real DemoDeps.
type recordingDeps struct {
	step.Deps
	summary, riskTier   string
	service             schema.Service
	op                  string
	params              map[string]any
	prompt              string
	model               schema.Model
	message, channel    string
	namespace, question string
}

func (d *recordingDeps) Approve(summary, riskTier string) (bool, string, error) {
	d.summary, d.riskTier = summary, riskTier
	return true, "recorder", nil
}
func (d *recordingDeps) ServiceCall(svc schema.Service, op string, params map[string]any) (any, error) {
	d.service, d.op, d.params = svc, op, params
	return "ok", nil
}
func (d *recordingDeps) AIGenerate(prompt string, model schema.Model) (string, error) {
	d.prompt, d.model = prompt, model
	return "ok", nil
}
func (d *recordingDeps) Notify(message, channel string) error {
	d.message, d.channel = message, channel
	return nil
}
func (d *recordingDeps) MemoryRecall(namespace, question string) (string, error) {
	d.namespace, d.question = namespace, question
	return "", nil
}

// Every argument a bot sends has to arrive. That sounds too obvious to
// test, and it is exactly what broke: the approve handler decoded into an
// untagged RiskTier while the wire carries risk_tier, and encoding/json
// matches field names case-insensitively but not across an underscore. So
// every approval that crossed a container boundary — which is every real
// one — lost its risk tier, and the human deciding whether to send twenty
// emails was shown no risk level at all. Nothing failed; the gate still
// gated. It only surfaced because the CLI started printing "unspecified
// risk" where it used to print an empty pair of brackets.
//
// The check is per-argument rather than per-handler for that reason: a
// dropped field is invisible unless something looks at the field.
func TestEveryCallbackArgumentArrives(t *testing.T) {
	host := &recordingDeps{Deps: demoDepsWithRecall(t, "")}
	remote := remoteThrough(t, host)

	t.Run("approve", func(t *testing.T) {
		if _, _, err := remote.Approve("Send 20 reminders", "high"); err != nil {
			t.Fatal(err)
		}
		if host.summary != "Send 20 reminders" {
			t.Errorf("summary = %q", host.summary)
		}
		if host.riskTier != "high" {
			t.Errorf("risk_tier = %q, want high — the human is shown this to decide", host.riskTier)
		}
	})

	t.Run("service.call", func(t *testing.T) {
		svc := schema.Service{ID: "gmail", Provider: "google", Connection: "demo"}
		if _, err := remote.ServiceCall(svc, "messages.list", map[string]any{"max": float64(200)}); err != nil {
			t.Fatal(err)
		}
		if host.service.ID != "gmail" || host.service.Provider != "google" || host.service.Connection != "demo" {
			t.Errorf("service = %#v — a dropped Connection would run a demo bot live", host.service)
		}
		if host.op != "messages.list" {
			t.Errorf("op = %q", host.op)
		}
		if host.params["max"] != float64(200) {
			t.Errorf("params = %#v", host.params)
		}
	})

	t.Run("ai.generate", func(t *testing.T) {
		model := schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6", MaxTokens: 4000, Temperature: 0.2}
		if _, err := remote.AIGenerate("hello", model); err != nil {
			t.Fatal(err)
		}
		if host.prompt != "hello" {
			t.Errorf("prompt = %q", host.prompt)
		}
		// max_tokens and temperature are the two that would silently become
		// zero, turning every generation deterministic and truncated.
		if host.model != model {
			t.Errorf("model = %#v, want %#v", host.model, model)
		}
	})

	t.Run("notify", func(t *testing.T) {
		if err := remote.Notify("done", "#ops"); err != nil {
			t.Fatal(err)
		}
		if host.message != "done" || host.channel != "#ops" {
			t.Errorf("message=%q channel=%q", host.message, host.channel)
		}
	})

	t.Run("memory.recall", func(t *testing.T) {
		if _, err := remote.MemoryRecall("inbox-triage", "what is urgent?"); err != nil {
			t.Fatal(err)
		}
		if host.namespace != "inbox-triage" || host.question != "what is urgent?" {
			t.Errorf("namespace=%q question=%q", host.namespace, host.question)
		}
	})
}

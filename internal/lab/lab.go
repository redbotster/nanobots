// Package lab is the "Lab" tier from context/TEAM-LAB-DESIGN.md: Human ->
// Lab Agent -> Team agents -> nanobots/nanoswarms. A human sends one chat
// message; Lab's orchestrator decides whether to delegate it to a Team
// role (internal/team), report on a role's recent work, or just answer in
// chat — never a fourth option that acts directly, since that would be
// exactly the elevated-trust shortcut context/TEAM-LAB-DESIGN.md rules out.
//
// The orchestrator is a single-shot router, not a multi-turn tool-calling
// loop: internal/llm.Generator is one prompt in, one completion out (the
// same constraint the AI composer already lives with — see
// internal/api/compose.go's own doc comment), and there is no
// function-calling client in this build to change that. One structured
// JSON decision per message, restating the conversation so far each time,
// is what the composer already does successfully for a harder version of
// the same problem (drafting a whole swarm), so Lab does the same thing
// for a smaller one.
package lab

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/team"
)

// labModel mirrors compose.go's composeModel — same reasoning: Provider
// and Name are a routing hint the configured Generator may or may not use
// (a direct provider client is already bound to its own model), not a
// promise this call reaches Anthropic specifically.
var labModel = schema.Model{Provider: "anthropic", Name: "claude-sonnet-4-6", MaxTokens: 1500}

// Config is what a Session needs that doesn't change message to message.
type Config struct {
	Team team.Config
	// DefaultEngine is which Team engine a delegation uses. Chosen once,
	// by whoever wires up the daemon, from which key is actually
	// configured — see wiring.go's doc comment for why this isn't the
	// model's own decision to make.
	DefaultEngine team.Engine
}

// turn is one exchange in the conversation, restated into every routing
// prompt — Generate has no memory of its own between calls.
type turn struct {
	who  string // "human" or "lab"
	text string
}

// Session is one ongoing Lab conversation. There is exactly one per
// server process today (see api.Server.Lab) — no multi-session story yet,
// matching this build's existing single-tenant shape everywhere else.
type Session struct {
	// run carries the conversation's log the same way a swarm run or a
	// foundry job does — Subscribe/Unsubscribe/LogEntries already exist on
	// *runner.Run and the WebUI already knows how to tail them over SSE
	// (see internal/api/sse.go), so reusing it here is one pub-sub
	// mechanism for the whole app rather than a second one built for chat
	// specifically. Nothing about it being called a "run" is meaningful;
	// no swarm executes because a chat session exists.
	run *runner.Run

	mu      sync.Mutex
	history []turn

	gen llm.Generator
	cfg Config
}

// NewSession starts one Lab conversation. gen is nil-checked by
// HandleMessage, not here, so a Session can exist (and its run can be
// read) before a model is configured — matching how the composer reports
// "no model configured" as a normal response rather than refusing to
// exist.
func NewSession(gen llm.Generator, cfg Config) *Session {
	run := runner.NewRun("lab")
	run.SetStatus(runner.StatusRunning)
	return &Session{run: run, gen: gen, cfg: cfg}
}

// Run exposes the session's log/subscribe surface to the API layer — see
// internal/api/lab.go.
func (s *Session) Run() *runner.Run { return s.run }

// HandleMessage routes one human message and acts on the decision,
// logging every step onto the session's run as it happens. Errors are
// logged rather than returned where a human is watching a chat, not
// polling a Go error value — the same reasoning runner.Run.Log already
// applies to a bot's own failures.
func (s *Session) HandleMessage(ctx context.Context, message string) {
	s.run.Log("you", "", "%s", message)
	roleNames, _ := team.Roles(s.cfg.Team.TeamDir) // no roles yet is not an error worth surfacing
	s.mu.Lock()
	s.history = append(s.history, turn{who: "human", text: message})
	history := append([]turn(nil), s.history...)
	s.mu.Unlock()

	if s.gen == nil {
		reply := "I don't have a model configured yet — set ONECLAW_API_KEY, or one of ANTHROPIC_API_KEY / OPENAI_API_KEY / GEMINI_API_KEY (see docs/llm.md), then talk to me again."
		s.appendLab(reply)
		return
	}

	d, err := route(ctx, s.gen, history, roleNames)
	if err != nil {
		s.appendLab(fmt.Sprintf("I couldn't decide what to do with that: %v", err))
		return
	}

	switch d.Action {
	case actionAnswer:
		s.appendLab(d.Text)
	case actionStatus:
		s.appendLab(s.reportStatus(d.Role))
	case actionDelegate:
		s.delegate(ctx, d.Role, d.Task)
	default:
		s.appendLab(fmt.Sprintf("I produced an action I don't recognize (%q) — that's a bug in me, not in you.", d.Action))
	}
}

// delegate runs the task through internal/team, streaming its live events
// onto the session's run exactly as they happen, then folds a short
// summary into the conversation so the next routing decision has
// something to go on — Generate still has no memory of the delegated
// run's outputs otherwise.
func (s *Session) delegate(ctx context.Context, role, task string) {
	if s.cfg.DefaultEngine == "" {
		s.appendLab("I'd delegate that, but no Team engine is configured — set ANTHROPIC_API_KEY or GEMINI_API_KEY in ~/.secrets/nanobots.env (see docs/team.md).")
		return
	}
	s.run.Log("lab", "", "delegating to %s: %s", role, task)

	events := make(chan foundry.Event)
	done := make(chan error, 1)
	go func() {
		done <- team.Run(ctx, s.cfg.Team, team.TaskInput{Role: role, Task: task, Engine: s.cfg.DefaultEngine}, events)
	}()

	summary, err := consumeDelegation(role, events, done, func(bot, phase, msg string) { s.run.Log(bot, phase, "%s", msg) })
	if err != nil {
		s.appendLab(fmt.Sprintf("%s hit a problem: %v", role, err))
		return
	}
	s.appendLab(fmt.Sprintf("%s: %s", role, summary))
}

// consumeDelegation drains one delegation's event stream, logging every
// event as it arrives via logf, and returns the accumulated final answer.
// Split out from delegate so this — the part with logic worth getting
// wrong — is testable with plain channels, no Docker and no team.Run.
//
// Gemini streams one answer as several delta chunks, each its own "text"
// event (docs/team.md's own transcript shows it: three events that only
// read as one sentence concatenated). Overwriting rather than
// accumulating — the first version of this kept only the last chunk, so
// Lab's summary read as a truncated fragment ("or created." instead of
// the whole answer) — was found by actually watching this happen in the
// browser, not by a test, because the only test at the time drove this
// through a fake that never streamed more than one event.
func consumeDelegation(role string, events <-chan foundry.Event, done <-chan error, logf func(bot, phase, msg string)) (string, error) {
	var text strings.Builder
	for {
		select {
		case ev := <-events:
			logf("team/"+role, ev.Phase, ev.Msg)
			if ev.Phase == "text" {
				text.WriteString(ev.Msg)
			}
		case err := <-done:
			if err != nil {
				return "", err
			}
			summary := text.String()
			if summary == "" {
				summary = "done, with no final message"
			}
			return summary, nil
		}
	}
}

func (s *Session) appendLab(text string) {
	s.run.Log("lab", "", "%s", text)
	s.mu.Lock()
	s.history = append(s.history, turn{who: "lab", text: text})
	s.mu.Unlock()
}

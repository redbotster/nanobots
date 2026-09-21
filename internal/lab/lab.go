// Package lab is the "Lab" tier from context/TEAM-LAB-DESIGN.md: Human ->
// Lab Agent -> Team agents -> nanobots/nanoswarms. A human sends one chat
// message; Lab's orchestrator decides whether to delegate it to a Team
// role (internal/team), report on a role's recent work, compose a starter
// automation, or just answer in chat.
//
// Composing is the one action that writes something without a Team
// agent's own workspace and review cycle in between, so it earns its own
// paragraph on why it still isn't the elevated-trust shortcut
// context/TEAM-LAB-DESIGN.md rules out: what it writes is a swarm file —
// the same artifact a Team agent editing its git worktree already
// produces today, and nothing in nanobots runs a swarm, or does anything
// to a real account, because its file exists on disk. A human still has
// to press Run, and any step with a real effect still opens its own
// approval gate first. Composing is exactly as safe as a Team agent
// authoring a swarm, minus the container and the wait — which is the
// whole point: "quickly build an example to try" reads as a promise about
// speed, and a general-purpose coding agent is not the fast path to it
// when a purpose-built one (the existing `/api/compose`) already is.
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

// AutomateResult is what one compose-and-save attempt produces — enough
// for Lab to describe it in chat and link to it. Defined here, not in
// internal/api, because internal/api already imports internal/lab (for
// Session itself) and a callback's return type has to live on whichever
// side of that import doesn't create a cycle.
type AutomateResult struct {
	// Gap is set instead of everything else when the real catalog cannot
	// do what was asked — composing's own honest "can't do this" outcome,
	// carried over rather than papered over with a swarm that doesn't work.
	Gap string
	// Name/Path/Description describe the swarm once Gap is empty. Path is
	// the same repo-relative form the WebUI already uses to open one
	// (examples/swarms/<slug>.yaml).
	Name        string
	Path        string
	Description string
	// PlanOK is false when the draft was saved anyway but doesn't fully
	// type-check yet — matching handleSaveSwarm's own policy of saving a
	// work in progress rather than blocking it, so Lab can say plainly
	// that there's one more thing to fix rather than claiming success.
	PlanOK    bool
	PlanError string
}

// Config is what a Session needs that doesn't change message to message.
type Config struct {
	Team team.Config
	// Engines resolves which Team engine a delegation uses — a global
	// default plus any per-role override, live-updatable from Settings
	// with no daemon restart. A human's choice, not the routing model's:
	// each engine spends a different real, metered API key, and Lab's
	// router already runs on a single-shot Generate with no memory of
	// what it decided last time — nothing here should let a model pick
	// which paid credential a request burns through.
	Engines *team.Preferences
	// Automate composes a swarm from a plain-English request and saves it
	// for real (see AutomateResult) — internal/api.Server's own compose +
	// save path, reused rather than reimplemented, injected here since
	// internal/lab cannot import internal/api (the reverse already holds:
	// api.Server carries a *lab.Session). nil disables the action entirely
	// — see automate()'s own nil check — matching how DefaultEngine's
	// absence disables delegation rather than panicking.
	Automate func(ctx context.Context, request string) (AutomateResult, error)
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
	case actionAutomate:
		s.automate(ctx, d.Request)
	default:
		s.appendLab(fmt.Sprintf("I produced an action I don't recognize (%q) — that's a bug in me, not in you.", d.Action))
	}
}

// automate composes a swarm from request and saves it for real — see
// AutomateResult and Config.Automate's own doc comments for why this one
// action is allowed to write something without a Team agent's workspace
// and review cycle in between.
func (s *Session) automate(ctx context.Context, request string) {
	if s.cfg.Automate == nil {
		s.appendLab("I can't build an automation yet — the composer needs a model configured (see docs/llm.md).")
		return
	}
	s.run.Log("lab", "composing", "composing an automation: %s", request)
	result, err := s.cfg.Automate(ctx, request)
	if err != nil {
		s.appendLab(fmt.Sprintf("I couldn't build that: %s", shortErr(err)))
		return
	}
	if result.Gap != "" {
		s.appendLab(fmt.Sprintf("The current bot catalog can't do that yet: %s", result.Gap))
		return
	}
	msg := fmt.Sprintf("Built %q and saved it.", result.Name)
	if result.PlanOK {
		msg += " Open it to see it run — nothing runs until you press Run yourself."
	} else {
		msg += fmt.Sprintf(" One thing doesn't connect yet: %s — open it to fix that first.", result.PlanError)
	}
	s.appendLabWithLink(msg, result.Path)
}

// delegate runs the task through internal/team, streaming its live events
// onto the session's run exactly as they happen, then folds a short
// summary into the conversation so the next routing decision has
// something to go on — Generate still has no memory of the delegated
// run's outputs otherwise.
func (s *Session) delegate(ctx context.Context, role, task string) {
	if s.cfg.Engines == nil || s.cfg.Engines.Default() == "" {
		s.appendLab("I'd delegate that, but no Team engine is configured — set ANTHROPIC_API_KEY or GEMINI_API_KEY in ~/.secrets/nanobots.env, or paste one in Settings (see docs/team.md).")
		return
	}
	engine := s.cfg.Engines.EngineFor(role)
	// step "delegating" marks this as an interim status line, not Lab's
	// answer — the WebUI treats a bare "lab"-authored entry (no step) as
	// the signal a message is fully answered (see LabPage.tsx), and this
	// line arrives well before that's true. Found live: without a way to
	// tell the two apart, the chat's "still working" indicator cleared the
	// instant this line appeared, while the actual delegation — the
	// container starting, the real work — was still 30-90 seconds out.
	s.run.Log("lab", "delegating", "delegating to %s: %s", role, task)

	events := make(chan foundry.Event)
	done := make(chan error, 1)
	go func() {
		done <- team.Run(ctx, s.cfg.Team, team.TaskInput{Role: role, Task: task, Engine: engine}, events)
	}()

	summary, err := consumeDelegation(role, events, done, func(bot, phase, msg string) { s.run.Log(bot, phase, "%s", msg) })
	if err != nil {
		// The full error, unedited, first — team.Run's error can carry a
		// whole container's stderr verbatim (RunDockerAgent wraps it in as
		// -is: a coding agent's actual output is the point of watching it
		// work, so nothing pre-filters it). Logged here under its own
		// phase so it's genuinely still readable in the chat log, not just
		// claimed to be, before appendLab's own message shortens it.
		s.run.Log("team/"+role, "error", "%s", err.Error())
		s.appendLab(fmt.Sprintf("%s hit a problem: %s", role, shortErr(err)))
		return
	}
	s.appendLab(fmt.Sprintf("%s: %s", role, summary))
}

// shortErr caps a delegation failure to something worth reading in Lab's
// own chat line. Found live, hitting Gemini's free-tier rate limit: with
// no cap, that line was a multi-hundred-line raw JavaScript stack trace,
// the actual 429 and its retry-after buried somewhere inside it.
func shortErr(err error) string {
	const max = 300
	s := err.Error()
	if len(s) <= max {
		return s
	}
	return s[:max] + "… (see the log above for the rest)"
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

// appendLabWithLink is appendLab plus a link to a swarm just composed and
// saved — see runner.Run.LogOpenSwarm.
func (s *Session) appendLabWithLink(text, swarmPath string) {
	s.run.LogOpenSwarm("lab", swarmPath, "%s", text)
	s.mu.Lock()
	s.history = append(s.history, turn{who: "lab", text: text})
	s.mu.Unlock()
}

package planner

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// UnfedInput is a required input port with nothing to fill it.
//
// A bot's required input can be satisfied three ways: an explicit value in
// the swarm's `inputs:` block, a snap from an upstream bot, or the port's
// own default. None of the three and the run fails — but it used to fail
// at run time, several containers in, with `resolve inputs: input "payload"
// has no explicit value, snap, or default`.
//
// lead-to-meeting shipped that way and nobody noticed, because `nanobots
// plan` said OK: the snaps all type-checked and the DAG had no cycles, and
// neither of those questions is "does every bot have what it needs". It's a
// plan-time property in every sense — nothing about it depends on data —
// and checking it at plan time is the difference between a message before
// anything runs and a half-finished swarm.
type UnfedInput struct {
	BotID string
	Port  string
	// Why is the specific reason, which is worth distinguishing: a
	// webhook-triggered swarm's entry bot is *designed* to be fed by the
	// trigger, and this build doesn't deliver webhooks. That is a different
	// problem from forgetting a snap, and sends the reader somewhere else.
	Why string
}

func (u UnfedInput) Error() string {
	return fmt.Sprintf("%s.%s is required but nothing supplies it — %s", u.BotID, u.Port, u.Why)
}

// CheckRequiredInputs finds every required input port with no value source.
func CheckRequiredInputs(rs *ResolvedSwarm) []UnfedInput {
	// Which ports have a snap arriving.
	snapped := map[string]bool{}
	for _, snap := range rs.Swarm.Spec.Snaps {
		to, err := ParseEndpoint(snap.To)
		if err != nil {
			continue
		}
		snapped[to.BotID+"."+to.Port] = true
	}

	trigger := strings.ToLower(rs.Swarm.Spec.Trigger.Type)
	fedByTrigger := trigger == "webhook" || trigger == "event"

	var out []UnfedInput
	for _, ref := range rs.Swarm.Spec.Bots {
		rb, ok := rs.Bots[ref.ID]
		if !ok || rb.Nanobot == nil {
			continue
		}
		for _, port := range rb.Nanobot.Spec.Ports.Inputs {
			if !port.Required {
				continue
			}
			if _, explicit := ref.Inputs[port.Name]; explicit {
				continue
			}
			if snapped[ref.ID+"."+port.Name] || port.Default != "" {
				continue
			}
			why := "give it a value in the swarm's inputs:, snap it from an upstream bot, or give the port a default"
			if fedByTrigger && !hasIncomingSnap(rs, ref.ID) {
				// The entry bot of a webhook/event swarm: the trigger is
				// meant to carry this, and this build never delivers one.
				why = fmt.Sprintf("this swarm's %s trigger carries it as {{trigger.payload}} (docs/webhooks.md)"+
					" — read it from there, and give the template a `| default:` so the swarm is still"+
					" runnable by hand when nothing has fired", trigger)
			}
			out = append(out, UnfedInput{BotID: ref.ID, Port: port.Name, Why: why})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BotID != out[j].BotID {
			return out[i].BotID < out[j].BotID
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// hasIncomingSnap reports whether anything feeds this bot at all — the
// difference between "the entry point" and "a bot in the middle someone
// forgot to wire".
func hasIncomingSnap(rs *ResolvedSwarm, botID string) bool {
	for _, snap := range rs.Swarm.Spec.Snaps {
		if to, err := ParseEndpoint(snap.To); err == nil && to.BotID == botID {
			return true
		}
	}
	return false
}

// CheckOnError rejects an on_error value that isn't one of the two.
//
// A typo here is uniquely bad: `on_error: contninue` reads as "keep going"
// to whoever wrote it, and silently means "stop" to the runner. It would
// only ever be noticed on the night something failed and the whole run
// went down anyway.
func CheckOnError(rs *ResolvedSwarm) []error {
	var out []error
	for _, b := range rs.Swarm.Spec.Bots {
		switch b.OnError {
		case "", schema.OnErrorStop, schema.OnErrorContinue:
		default:
			out = append(out, fmt.Errorf("bot %q has on_error: %q — it must be %q or %q (default %q)",
				b.ID, b.OnError, schema.OnErrorStop, schema.OnErrorContinue, schema.OnErrorStop))
		}
	}
	return out
}

// CheckSnapAndValueCollision finds a port that is both snapped into and
// given an explicit value.
//
// The runner resolves explicit inputs first, so the snap is silently
// dropped: the swarm diagram shows a wire, the plan type-checks it, and at
// run time it carries nothing. There is no reading of that which is what
// someone meant — either the wire is real or the literal is.
//
// Found in a composed draft, which set notifier.message to a fixed string
// *and* snapped a summary into it. The plan said OK. The Slack message
// would have been the fixed string forever.
func CheckSnapAndValueCollision(rs *ResolvedSwarm) []error {
	snapped := map[string]bool{}
	for _, snap := range rs.Swarm.Spec.Snaps {
		if to, err := ParseEndpoint(snap.To); err == nil {
			snapped[to.BotID+"."+to.Port] = true
		}
	}
	var out []error
	for _, b := range rs.Swarm.Spec.Bots {
		names := make([]string, 0, len(b.Inputs))
		for name := range b.Inputs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if snapped[b.ID+"."+name] {
				out = append(out, fmt.Errorf(
					"%s.%s has both a snap and a value in inputs: — the value wins and the snap is"+
						" silently dropped, so remove whichever one you did not mean", b.ID, name))
			}
		}
	}
	return out
}

// maxRetry is a ceiling on retry, not a policy. Three attempts rides out a
// blip; thirty is a bot hammering someone's API while a human watches a
// spinner.
const maxRetry = 3

// maxRetryBackoff caps the wait between retries. A minute is already long
// enough that a human staring at a spinner has gone to do something else;
// past that, the honest answer is on_error: continue plus a notification,
// not a longer sleep.
const maxRetryBackoff = 60 * time.Second

// CheckRetry rejects a retry count that isn't sane, and refuses one on a
// bot that writes somewhere.
//
// A retry re-runs the *entire bot*. A bot that sent an email and then
// failed on its last step will send that email again — so retry on a bot
// declaring guardrails.writes_allowed is a duplicate-send waiting to
// happen, and the swarm author almost certainly did not mean it. Refused at
// plan time rather than warned about at 3am.
func CheckRetry(rs *ResolvedSwarm) []error {
	var out []error
	for _, b := range rs.Swarm.Spec.Bots {
		if b.Retry == 0 {
			continue
		}
		if b.Retry < 0 || b.Retry > maxRetry {
			out = append(out, fmt.Errorf("bot %q has retry: %d — it must be between 0 and %d",
				b.ID, b.Retry, maxRetry))
			continue
		}
		rb, ok := rs.Bots[b.ID]
		if !ok || rb.Nanobot == nil {
			continue
		}
		if w := rb.Nanobot.Spec.Guardrails.WritesAllowed; len(w) > 0 {
			out = append(out, fmt.Errorf(
				"bot %q has retry: %d, but it writes to %s — a retry re-runs the whole bot, so a"+
					" failure after the write sends it again. Remove the retry, or split the write"+
					" into its own bot that isn't retried",
				b.ID, b.Retry, strings.Join(w, ", ")))
		}
	}
	return out
}

// CheckRetryBackoff rejects a retry_backoff that cannot mean anything: set
// with no retry to space out, not a duration at all, negative, or long
// enough that it stops looking like "wait out a blip" and starts looking
// like a second scheduler.
func CheckRetryBackoff(rs *ResolvedSwarm) []error {
	var out []error
	for _, b := range rs.Swarm.Spec.Bots {
		if b.RetryBackoff == "" {
			continue
		}
		if b.Retry <= 0 {
			out = append(out, fmt.Errorf(
				"bot %q has retry_backoff: %q but retry: %d — nothing to wait between, set retry"+
					" first or remove retry_backoff", b.ID, b.RetryBackoff, b.Retry))
			continue
		}
		d, err := time.ParseDuration(b.RetryBackoff)
		if err != nil {
			out = append(out, fmt.Errorf("bot %q has retry_backoff: %q — not a duration (try %q): %w",
				b.ID, b.RetryBackoff, "5s", err))
			continue
		}
		if d <= 0 {
			out = append(out, fmt.Errorf("bot %q has retry_backoff: %q — must be positive",
				b.ID, b.RetryBackoff))
			continue
		}
		if d > maxRetryBackoff {
			out = append(out, fmt.Errorf("bot %q has retry_backoff: %q — must be %s or less",
				b.ID, b.RetryBackoff, maxRetryBackoff))
		}
	}
	return out
}

// CheckWhen rejects a when: condition that cannot mean anything: a
// reference to something other than this bot's own inputs, a reference to
// an input port the bot doesn't declare, or a comparison between two
// literals that aren't both numbers. Everything else — whether the
// condition is actually true — can only be known once the run has real
// data, so this is the plan-time half of the same typo-catching the other
// checks in this file do.
func CheckWhen(rs *ResolvedSwarm) []error {
	var out []error
	for _, b := range rs.Swarm.Spec.Bots {
		if b.When == "" {
			continue
		}
		parsed, err := step.ParseCondition(b.When)
		if err != nil {
			out = append(out, fmt.Errorf("bot %q has when: %q — %w", b.ID, b.When, err))
			continue
		}
		rb, ok := rs.Bots[b.ID]
		if !ok || rb.Nanobot == nil {
			continue // an unresolved bot is reported elsewhere
		}
		ports := map[string]bool{}
		for _, p := range rb.Nanobot.Spec.Ports.Inputs {
			ports[p.Name] = true
		}
		var badPath string
		for _, path := range step.TemplatePaths(parsed.Left) {
			if !validWhenPath(path, ports) {
				badPath = path
				break
			}
		}
		if badPath == "" {
			for _, path := range step.TemplatePaths(parsed.Right) {
				if !validWhenPath(path, ports) {
					badPath = path
					break
				}
			}
		}
		if badPath != "" {
			out = append(out, fmt.Errorf(
				"bot %q has when: %q — %q is not one of this bot's own inputs; when: can only"+
					" test a port this bot already has, as {{inputs.<port>}}", b.ID, b.When, badPath))
			continue
		}
		// A comparison between two literals (no {{...}} on either side) is
		// knowable right now, not just at run time — and if it isn't both
		// numbers, the run would refuse it every single time.
		if parsed.Op != "" && parsed.Op != "==" && parsed.Op != "!=" &&
			len(step.TemplatePaths(parsed.Left)) == 0 && len(step.TemplatePaths(parsed.Right)) == 0 {
			if _, err := step.EvalCondition(b.When, nil); err != nil {
				out = append(out, fmt.Errorf("bot %q has when: %q — %w", b.ID, b.When, err))
			}
		}
	}
	return out
}

// CheckFallback rejects a fallback: bot that cannot actually stand in for
// the one it replaces: one that doesn't resolve, one that is the same bot
// it's already using, one whose output ports don't match name-for-name and
// type-for-type, or one that needs an input port this bot instance doesn't
// already have. The fallback runs with this instance's own resolved
// inputs — nothing is re-wired for it — and downstream snaps type-checked
// against the primary bot's ports, so a fallback with a different shape
// would make that type-check a lie.
func CheckFallback(rs *ResolvedSwarm) []error {
	var out []error
	for _, b := range rs.Swarm.Spec.Bots {
		if b.Fallback == "" {
			continue
		}
		rb, ok := rs.Bots[b.ID]
		if !ok || rb.Nanobot == nil {
			continue // an unresolved primary bot is reported elsewhere
		}
		fb, ok := rs.Fallbacks[b.ID]
		if !ok || fb.Nanobot == nil {
			out = append(out, fmt.Errorf("bot %q has fallback: %q, which did not resolve", b.ID, b.Fallback))
			continue
		}
		if b.Fallback == b.Use {
			out = append(out, fmt.Errorf(
				"bot %q has fallback: %q, the same bot it already uses — a bot cannot fall back to itself",
				b.ID, b.Fallback))
			continue
		}

		primaryOut := rb.Nanobot.Spec.Ports.Outputs
		fallbackOut := fb.Nanobot.Spec.Ports.Outputs
		if len(primaryOut) != len(fallbackOut) {
			out = append(out, fmt.Errorf(
				"bot %q has fallback: %q with %d output port(s), but %q declares %d — they must match"+
					" exactly, or a downstream snap that type-checked against one would silently break"+
					" against the other", b.ID, b.Fallback, len(fallbackOut), b.Use, len(primaryOut)))
			continue
		}
		fallbackByName := make(map[string]schema.OutputPort, len(fallbackOut))
		for _, p := range fallbackOut {
			fallbackByName[p.Name] = p
		}
		for _, p := range primaryOut {
			fp, ok := fallbackByName[p.Name]
			switch {
			case !ok:
				out = append(out, fmt.Errorf(
					"bot %q has fallback: %q, which has no output port named %q — %q needs it for"+
						" downstream snaps to keep working", b.ID, b.Fallback, p.Name, b.Use))
			case fp.Type != p.Type:
				out = append(out, fmt.Errorf(
					"bot %q has fallback: %q, whose %q output is %q, not %q like %q's",
					b.ID, b.Fallback, p.Name, fp.Type, p.Type, b.Use))
			}
		}

		primaryIn := map[string]bool{}
		for _, p := range rb.Nanobot.Spec.Ports.Inputs {
			primaryIn[p.Name] = true
		}
		for _, p := range fb.Nanobot.Spec.Ports.Inputs {
			if p.Required && !primaryIn[p.Name] {
				out = append(out, fmt.Errorf(
					"bot %q has fallback: %q, which requires input %q that %q doesn't have — the"+
						" fallback runs with this bot instance's own resolved inputs, so it can't ask"+
						" for a port that was never there", b.ID, b.Fallback, p.Name, b.Use))
			}
		}
	}
	return out
}

// maxLoop bounds bounded iteration for the same reason maxRetry does: an
// unbounded "while" against someone else's API is not a thing this runs
// unattended. Twenty pages is deep enough for the catalog's own pagination
// jobs and shallow enough that a mistaken condition fails fast rather than
// running all night.
const maxLoop = 20

// CheckLoop rejects a loop: that can't mean anything: a max outside 1..20, a
// feed: mapping that names a port this bot doesn't declare on either side,
// an until: that can't parse or that reaches past this bot's own outputs, or
// a bot that also fans out — the two mean two different multiplicities for
// the same bot instance's outputs, and letting both apply would leave
// downstream typed against whichever one was checked last.
func CheckLoop(rs *ResolvedSwarm) []error {
	var out []error
	for _, b := range rs.Swarm.Spec.Bots {
		if b.Loop == nil {
			continue
		}
		if b.Loop.Max < 1 || b.Loop.Max > maxLoop {
			out = append(out, fmt.Errorf("bot %q has loop.max: %d — it must be between 1 and %d",
				b.ID, b.Loop.Max, maxLoop))
			continue
		}
		rb, ok := rs.Bots[b.ID]
		if !ok || rb.Nanobot == nil {
			continue // an unresolved bot is reported elsewhere
		}
		if fo, err := FanOutFor(rs.Swarm, b.ID); err == nil && fo != nil {
			out = append(out, fmt.Errorf(
				"bot %q has both loop: and a fanned-out snap (%s) — they can't both decide how many"+
					" of this bot's outputs downstream sees", b.ID, fo.Over))
			continue
		}

		inputs := map[string]bool{}
		for _, p := range rb.Nanobot.Spec.Ports.Inputs {
			inputs[p.Name] = true
		}
		outputs := map[string]bool{}
		for _, p := range rb.Nanobot.Spec.Ports.Outputs {
			outputs[p.Name] = true
		}
		feedInputs := make([]string, 0, len(b.Loop.Feed))
		for in := range b.Loop.Feed {
			feedInputs = append(feedInputs, in)
		}
		sort.Strings(feedInputs)
		for _, in := range feedInputs {
			outPort := b.Loop.Feed[in]
			if !inputs[in] {
				out = append(out, fmt.Errorf(
					"bot %q has loop.feed: %q is not one of its own input ports", b.ID, in))
			}
			if !outputs[outPort] {
				out = append(out, fmt.Errorf(
					"bot %q has loop.feed: %q -> %q, but %q is not one of its own output ports",
					b.ID, in, outPort, outPort))
			}
		}

		if b.Loop.Until == "" {
			continue
		}
		parsed, err := step.ParseCondition(b.Loop.Until)
		if err != nil {
			out = append(out, fmt.Errorf("bot %q has loop.until: %q — %w", b.ID, b.Loop.Until, err))
			continue
		}
		var badPath string
		for _, path := range append(step.TemplatePaths(parsed.Left), step.TemplatePaths(parsed.Right)...) {
			if !validLoopUntilPath(path, outputs) {
				badPath = path
				break
			}
		}
		if badPath != "" {
			out = append(out, fmt.Errorf(
				"bot %q has loop.until: %q — %q is not one of this bot's own outputs; loop.until can"+
					" only test a port this bot itself produces, as {{outputs.<port>}}",
				b.ID, b.Loop.Until, badPath))
		}
	}
	return out
}

// CheckExecution validates schema.Harness.Execution and schema.BotRef.Execution
// (v3 Phase 1). Both fields are one-directional on purpose — see
// Harness.Execution's doc comment — so the only two legal values anywhere
// are "" and "container"; anything else, including "inprocess", is rejected
// here rather than silently ignored or silently honoured.
func CheckExecution(rs *ResolvedSwarm) []error {
	var out []error
	validate := func(where, value string) {
		if value != "" && value != "container" {
			out = append(out, fmt.Errorf(
				"%s has execution: %q — the only legal values are \"container\" or omitting it entirely "+
					"(there is no way to force a bot out of a container it otherwise needs)", where, value))
		}
	}
	for _, b := range rs.Swarm.Spec.Bots {
		validate(fmt.Sprintf("bot %q", b.ID), b.Execution)
		if rb, ok := rs.Bots[b.ID]; ok && rb.Nanobot != nil {
			validate(fmt.Sprintf("bot %q's catalog definition (%s)", b.ID, rb.Nanobot.Metadata.Name), rb.Nanobot.Spec.Harness.Execution)
		}
	}
	return out
}

// validLoopUntilPath is the one thing a loop.until condition may reference:
// this bot's own most recent output, by the exact name it declared.
func validLoopUntilPath(path string, outputs map[string]bool) bool {
	port, ok := strings.CutPrefix(path, "outputs.")
	if !ok {
		return false
	}
	return outputs[port]
}

// validWhenPath is the one thing a when: condition may reference: this
// bot's own resolved input, by the exact name it declared. Nothing else —
// vars, trigger, another bot's id — is available at the point when: is
// evaluated (see internal/runner.whenGate), so allowing the syntax here
// would parse a condition that can never do anything at run time.
func validWhenPath(path string, ports map[string]bool) bool {
	port, ok := strings.CutPrefix(path, "inputs.")
	if !ok {
		return false
	}
	return ports[port]
}

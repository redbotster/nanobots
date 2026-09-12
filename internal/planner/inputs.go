package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
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
				why = fmt.Sprintf("this swarm's %s trigger would carry it, and %s triggers aren't delivered in this build"+
					" (docs/scheduler.md) — give it an example value in the swarm's inputs: so it can be run by hand",
					trigger, trigger)
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

// Package remedy turns a run's failure into the thing to do about it.
//
// A run that went red says what happened. It rarely says what to do, and in
// this product the answer is usually one click away in Settings — six of the
// catalog swarms fail on a single missing Slack token, and the error
// politely says "connect it from Settings" while leaving you to go find it.
//
// This lived only in the WebUI (web/src/lib/runError.ts) for its first life,
// which meant `nanobots run` — a first-class command that runs a swarm to
// completion and prompts on approvals — printed a bare `error: ...` for
// every one of these. A locked vault, a missing model, an unconnected
// account: all of them had a known, one-sentence fix that a terminal user
// was never shown. CLAUDE.md asks that "a failure says what to do about
// it", and for the CLI it did not.
//
// So the table lives here now and both callers read it: the CLI prints it
// after the error, and the API hands it to the WebUI on the run. One place
// per fact, rather than a Go copy and a TypeScript copy drifting apart.
//
// Deliberately conservative. Every entry is a failure actually seen in this
// build, matched on wording the server itself controls; anything
// unrecognised gets no remedy rather than a guess. A confidently wrong
// suggestion is worse than none — it sends someone to reconfigure a thing
// that was never the problem.
package remedy

import (
	"fmt"
	"regexp"
	"strings"
)

// Remedy is what to do about a failure.
type Remedy struct {
	// Advice is one sentence: what to do about it.
	Advice string `json:"advice"`
	// Action is a Settings-level fix, when there is one. Page is a WebUI
	// destination; the CLI ignores it and prints the advice alone, since
	// "click Settings" means nothing in a terminal.
	Action *Action `json:"action,omitempty"`
	// Docs is where to read more, when the fix is not a button.
	Docs string `json:"docs,omitempty"`
}

type Action struct {
	Label string `json:"label"`
	Page  string `json:"page"`
}

// connectable is the services with a connect flow in Settings, as the server
// names them in a vault path.
var connectable = map[string]string{
	"google":   "Google",
	"slack":    "Slack",
	"github":   "GitHub",
	"stripe":   "Stripe",
	"hubspot":  "HubSpot",
	"x":        "X",
	"linkedin": "LinkedIn",
}

// secretPath matches the service in `Secret slack/bot_token not found`.
var secretPath = regexp.MustCompile(`secret ([a-z_]+)/`)

func settings(label string) *Action { return &Action{Label: label, Page: "settings"} }

// For returns the remedy for a raw run error, or nil when there isn't a
// known one.
//
// Order matters and is preserved from the original: an unattended decline is
// also "not approved", and a timed-out approval is neither, so the specific
// cases are checked before the general one.
func For(raw string) *Remedy {
	m := strings.ToLower(Message(raw))
	if m == "" {
		return nil
	}

	// A missing credential names the service in the secret path.
	if sm := secretPath.FindStringSubmatch(m); sm != nil {
		if service, ok := connectable[sm[1]]; ok {
			return &Remedy{
				Advice: fmt.Sprintf(
					"No %s account is connected yet, so this bot had no credential to use.", service),
				Action: settings("Connect " + service),
			}
		}
	}
	if strings.Contains(m, "no connected account yet") {
		return &Remedy{
			Advice: "No account is connected for this service yet, so this bot had no credential to use.",
			Action: settings("Open Settings"),
		}
	}

	// The vault re-locks on its own schedule and the fix is on a phone, so
	// there is no button to offer — only the right instruction.
	if strings.Contains(m, "vault is locked") || strings.Contains(m, "passkey verification required") {
		return &Remedy{
			Advice: "The 1Claw vault re-locked. Unlock it with your passkey on your phone, " +
				"then run this again — nothing needs changing here.",
		}
	}

	if strings.Contains(m, "cannot connect to the docker daemon") || strings.Contains(m, "docker isn't") {
		return &Remedy{
			Advice: "Every bot runs in a container. Start Docker Desktop and run this again.",
		}
	}

	if strings.Contains(m, "no llm is configured") {
		return &Remedy{
			Advice: "No model is configured, so this bot had nothing to generate with.",
			Action: settings("Set up a model"),
		}
	}

	// Checked before the general "not approved" below: an unattended decline
	// is also "not approved", and the generic advice ("run it again to be
	// asked afresh") is wrong when there was never anything to ask.
	if strings.Contains(m, "no terminal attached") {
		return &Remedy{
			Advice: "Nothing was attached to answer the approval, so it was declined " +
				"automatically. Run it from the app, or from a terminal where you can answer.",
		}
	}

	// Nobody was there. Distinct from a decline (a decision) and from a hang
	// (a fault): the run did everything right and then waited for a person
	// who never came. The most common failure on a machine running scheduled
	// swarms — 54 runs on the development machine, 27 hours of container
	// time. "Run it again" is deliberately not the advice: the next
	// scheduled run at 7am goes unanswered exactly the same way.
	if strings.Contains(m, "nobody answered") {
		return &Remedy{
			Advice: "The bot asked for approval and nobody answered in time, so it was " +
				"stopped. If this runs on a schedule while you're away, connect 1Claw so " +
				"the question reaches your phone — or take the approval off this step if " +
				"it doesn't need one.",
			Action: settings("Open Settings"),
			Docs:   "docs/approvals.md",
		}
	}

	// A declined approval is a decision, not a fault — saying so stops
	// someone debugging their own "no".
	if strings.Contains(m, "not approved") {
		return &Remedy{
			Advice: "This wasn't a fault: the approval was declined, so the bot stopped " +
				"before doing anything. Run it again to be asked afresh.",
		}
	}

	if strings.Contains(m, "has no explicit value, snap, or default") {
		return &Remedy{
			Advice: "A bot needs an input nothing supplies. Give it a value on the bot, " +
				"or snap it from an upstream bot, in the builder.",
		}
	}

	if strings.Contains(m, "they iterate together") {
		return &Remedy{
			Advice: "Two lists feeding one fanned-out bot came back different lengths. " +
				"Snap both sides from a single list whose items carry everything the bot needs.",
			Docs: "docs/fan-out.md",
		}
	}

	if strings.Contains(m, "429") || strings.Contains(m, "rate limit") || strings.Contains(m, "quota") {
		return &Remedy{
			Advice: "The model provider is rate-limiting or out of quota. Wait a minute and " +
				"run it again, or point NANOBOTS_LLM at a provider with more headroom.",
			Docs: "docs/llm.md",
		}
	}

	return nil
}

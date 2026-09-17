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

	// "Every bot runs in a container" was true when this was written and
	// stopped being true when 34 of the 39 moved in-process. The same
	// sentence was in the banner across every page and in Settings; this
	// was the third copy, and the one a person reads on the run that
	// actually failed.
	if strings.Contains(m, "cannot connect to the docker daemon") || strings.Contains(m, "docker isn't") {
		return &Remedy{
			Advice: "This bot renders a PDF or a chart, which needs a real browser in a container. " +
				"Start Docker Desktop and run it again — the rest of the catalog runs without it.",
			Docs: "docs/architecture.md",
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
		// The advice here has been wrong in both directions. It first said
		// "connect 1Claw so the question reaches your phone", to an account
		// that already had 1Claw connected and a mirror that could not work
		// — unactionable, and it sent someone to check a setting that was
		// never the problem. It was then corrected to say the window is the
		// only lever, which became untrue the moment the mirror started
		// working (see internal/runner/approver.go).
		//
		// So it says both, in the order that helps: the run is still over,
		// and the reason the next one does not have to be depends on whether
		// 1Claw is set up. The caller knows which; this does not, which is
		// why the second sentence is conditional rather than a promise.
		return &Remedy{
			Advice: "The bot asked for approval and nobody answered in time, so it was " +
				"stopped. An approval can only be answered while the run is waiting. With " +
				"1Claw connected the same question also goes to your 1Claw queue, so it can " +
				"be answered away from this tab; without it, move the schedule to a time you " +
				"are around, or take the approval off this step if it doesn't need one.",
			Docs: "docs/approvals.md",
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

	// The container's own ceiling, reached for a reason other than an
	// unanswered approval — describeTimeout renames that case before it
	// ever gets here, so what is left is a bot that genuinely sat there.
	//
	// 55 of the 97 failed runs on the development machine carried this
	// message with no advice attached, which made it the single largest
	// unanswered failure in the history.
	if strings.Contains(m, "exceeded") && strings.Contains(m, "was stopped") {
		return &Remedy{
			Advice: "The bot hit its own max_runtime_secs and was stopped. Its last log line is " +
				"where it got to: a model call that never returned, a page that never loaded, " +
				"or a gate nobody answered. Raise that budget in the bot's nanobot.yaml if the " +
				"work legitimately takes longer.",
			Docs: "docs/runs.md",
		}
	}

	// A fanned-out snap indexing into a list that came back empty. The
	// upstream bot succeeded and produced nothing, which is an ordinary
	// Tuesday rather than a fault — an inbox with no urgent mail, a
	// transcript with no decisions.
	if strings.Contains(m, "item(s), index") {
		return &Remedy{
			Advice: "An upstream bot produced an empty list, and this snap asks for an item by " +
				"position. Snap the whole list with `.*` so the bot runs once per item and not " +
				"at all when there are none.",
			Docs: "docs/fan-out.md",
		}
	}

	// web.fetch reached the page and the page said no.
	if strings.Contains(m, "web.fetch") && strings.Contains(m, "unexpected status") {
		return &Remedy{
			Advice: "The page answered with an error status. Check the URL in the swarm's inputs " +
				"opens in a browser — a placeholder or a moved page fails here every run, on a " +
				"schedule, forever.",
			Docs: "docs/connections.md",
		}
	}

	// The model wrote prose where the bot asked for JSON. Usually transient
	// — the same prompt on the same model answers correctly most of the
	// time — which is why "run it again" comes first and the prompt only
	// comes up if it keeps happening.
	if strings.Contains(m, "not valid json") {
		return &Remedy{
			Advice: "The model answered with prose where this bot asked for JSON. Run it again; " +
				"if it keeps happening, the bot's prompt needs to insist harder on raw JSON.",
			Docs: "docs/llm.md",
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

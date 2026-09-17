package step

import "fmt"

// checkParams refuses a call the bot has clearly not filled in.
//
// It exists because of one measured run. `lead-to-meeting` snaps a lead
// record (`{name, email, company, notes}`) into calendar-scheduler's
// `thread` port, whose prompt says it needs "at least from, subject,
// snippet". Both ports are `json`, so the planner type-checks it; neither
// field exists, so the bot drafted an email to `""` with the subject
// `"Re: "` — and every layer above reported success. The swarm has run on a
// webhook trigger for weeks, drafting blank emails and then asking a human
// to approve sending them.
//
// Demo mode is where this hides. DemoDeps returns the fixture whatever the
// params are, on purpose — a fixture describes an answer, not a request — so
// nothing downstream of the call can see that nothing went into it. That is
// why this sits in the interpreter, before dispatch, rather than in either
// backend: it is a check on what the bot asked for, and both backends are
// asked the same way.
//
// Deliberately tiny, and the same rule remedy.go follows: only what has
// actually been seen to break, matched on params this repo's own bots
// declare. A confidently-wrong required field would refuse a call that works
// today, which is worse than the blank draft.
func checkParams(service, op string, params map[string]any) error {
	switch op {
	case "drafts.create", "messages.send":
		// The one op in the catalog that composes a message from fields the
		// bot resolved itself, and therefore the one that can silently
		// resolve them to nothing.
		items, ok := params["drafts"].([]any)
		if !ok {
			return nil
		}
		for i, it := range items {
			d, _ := it.(map[string]any)
			to, _ := d["to"].(string)
			if to == "" {
				return fmt.Errorf(
					"%s.%s: draft %d has no recipient — whatever was snapped into this bot "+
						"does not carry the field its `to:` reads", service, op, i+1)
			}
		}
	}
	return nil
}

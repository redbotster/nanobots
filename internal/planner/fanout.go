package planner

import (
	"fmt"

	"github.com/redbotster/nanobots/internal/schema"
)

// FanOutMarker is the path segment that turns a snap into a fan-out edge:
//
//	from: chaser.overdue.*
//	to:   sender.invoice
//
// meaning "run `sender` once for every element of `chaser.overdue`", rather
// than the `chaser.overdue.0` this catalog has had to write everywhere —
// six swarms carry a comment apologising that only the first item is
// processed.
//
// `*` was chosen over `[]` because the path is already dot-separated and
// already understands numeric indices (`overdue.0`), so `*` reads as "every
// index" in exactly the same position, and it survives YAML unquoted.
const FanOutMarker = "*"

// FanOut describes how one bot instance is fanned out.
type FanOut struct {
	// Over is the endpoint whose list drives the iteration, as written.
	Over string
	// Snaps are every inbound snap on this bot that carries the marker.
	// All of them iterate together, in lockstep, over the same index.
	Snaps []schema.Snap
}

// FanOutFor returns how botID is fanned out, or nil if it isn't.
//
// Every marker-carrying snap into the same bot iterates in lockstep on one
// shared index — `chaser.overdue.*` and `chaser.draft_ids.*` feeding the
// same bot means element i of each, together, which is what "for each
// overdue invoice, with its draft" means. Two *different* upstream lists of
// different lengths would be a cross product, which is never what anyone
// means by "for each", so the runner requires equal lengths.
func FanOutFor(sw *schema.Nanoswarm, botID string) (*FanOut, error) {
	var fo *FanOut
	for _, snap := range sw.Spec.Snaps {
		to, err := ParseEndpoint(snap.To)
		if err != nil {
			return nil, err
		}
		if to.BotID != botID || !HasFanOutMarker(snap.From) {
			continue
		}
		if hasMarker(to.Fields) {
			return nil, fmt.Errorf("snap to %q: the %s marker belongs on the `from` side — it says which list to iterate, not where to put each item",
				snap.To, FanOutMarker)
		}
		if fo == nil {
			fo = &FanOut{Over: snap.From}
		}
		fo.Snaps = append(fo.Snaps, snap)
	}
	return fo, nil
}

// HasFanOutMarker reports whether an endpoint reference fans out.
func HasFanOutMarker(ref string) bool {
	ep, err := ParseEndpoint(ref)
	if err != nil {
		return false
	}
	return hasMarker(ep.Fields)
}

func hasMarker(fields []string) bool {
	for _, f := range fields {
		if f == FanOutMarker {
			return true
		}
	}
	return false
}

// IsFannedOut reports whether botID runs more than once — used to decide
// whether its *outputs* are lists, which is what makes the fan-out visible
// to everything downstream of it.
func IsFannedOut(sw *schema.Nanoswarm, botID string) bool {
	fo, err := FanOutFor(sw, botID)
	return err == nil && fo != nil
}

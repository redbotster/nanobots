package planner

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Joining is the inverse of fanning out.
//
// A bot that fans out runs once per item and produces one of each output
// per run, so the planner types its outputs as list<T> (see
// resolveEndpointType). A downstream bot that does *not* fan out wants one
// value — "send each of these twenty reminders, then post me one summary".
// Nothing bridged that, so three catalog swarms snapped `.0` and acted on
// the first item only, with a comment apologising for it.
//
// `join:` on the snap says how the list becomes one value. It is explicit
// rather than implicit on purpose: "twenty message ids became one string"
// is a real choice — newline-separated, a JSON array, or just the count are
// all reasonable — and a reader of the YAML should not have to know a rule
// to see which one happened.
//
// Both halves live here, next to each other, because they have to agree: a
// join whose declared type the planner accepts must be a join the runner
// can actually perform, or a swarm type-checks and then fails mid-run.

// JoinMode is the set of transforms a snap may name.
type JoinMode string

const (
	// JoinLines renders each item as text, one per line. The default shape
	// for "tell me what happened" — a Slack message, an email body.
	JoinLines JoinMode = "lines"
	// JoinJSON renders the whole list as a JSON array, for a downstream
	// bot that will parse it rather than read it.
	JoinJSON JoinMode = "json"
	// JoinCount is how many items there were.
	//
	// Produces a string, not a number, because this schema has no numeric
	// port type — and its use is "3 reminders sent" in a message rather
	// than arithmetic, which nothing here does.
	JoinCount JoinMode = "count"
	// JoinFlatten turns list<list<T>> into list<T>. What a fanned-out bot
	// whose own output is already a list needs: ten ideas each producing
	// three posts is thirty posts, not ten groups of three.
	JoinFlatten JoinMode = "flatten"
	// JoinFirst takes the first item — today's `.0`, named, so the choice
	// to ignore the rest is visible instead of looking like an index.
	JoinFirst JoinMode = "first"
)

var joinModes = map[JoinMode]bool{
	JoinLines: true, JoinJSON: true, JoinCount: true, JoinFlatten: true, JoinFirst: true,
}

// JoinModes lists every mode, sorted, for error messages and docs — from
// the map, so it cannot drift from what is implemented.
func JoinModes() []string {
	out := make([]string, 0, len(joinModes))
	for m := range joinModes {
		out = append(out, string(m))
	}
	sort.Strings(out)
	return out
}

// JoinType reports what a join produces, given what it consumes.
//
// Every mode requires a list, because joining anything else is a mistake
// worth catching at plan time: a `join:` on a snap that was never fanned
// out means the author expected a fan-out that isn't happening.
func JoinType(mode JoinMode, from schema.ParsedType) (schema.ParsedType, error) {
	str := schema.ParsedType{Base: schema.PortString}
	if !joinModes[mode] {
		return str, fmt.Errorf("unknown join %q (have: %s)", mode, strings.Join(JoinModes(), ", "))
	}
	if from.Base != "list" || from.List == nil {
		return str, fmt.Errorf("join: %s needs a list to join, but this side is %s —"+
			" a join belongs on a snap out of a bot that fans out", mode, from)
	}
	switch mode {
	case JoinLines, JoinJSON, JoinCount:
		return str, nil
	case JoinFirst:
		return *from.List, nil
	case JoinFlatten:
		if from.List.Base != "list" || from.List.List == nil {
			return str, fmt.Errorf("join: flatten needs list<list<...>>, but this side is %s", from)
		}
		return *from.List, nil
	}
	return str, fmt.Errorf("unhandled join %q", mode)
}

// JoinValue performs the join the type check promised.
func JoinValue(mode JoinMode, v any) (any, error) {
	items, ok := v.([]any)
	if !ok {
		// The planner should have caught this, so reaching it means the
		// two halves disagree — say so rather than producing a plausible
		// wrong answer.
		return nil, fmt.Errorf("join: %s expected a list at run time, got %T", mode, v)
	}
	switch mode {
	case JoinLines:
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, itemText(it))
		}
		return strings.Join(parts, "\n"), nil
	case JoinJSON:
		raw, err := json.Marshal(items)
		if err != nil {
			return nil, fmt.Errorf("join: json: %w", err)
		}
		return string(raw), nil
	case JoinCount:
		return strconv.Itoa(len(items)), nil
	case JoinFlatten:
		var out []any
		for _, it := range items {
			inner, ok := it.([]any)
			if !ok {
				return nil, fmt.Errorf("join: flatten expected every item to be a list, got %T", it)
			}
			out = append(out, inner...)
		}
		if out == nil {
			out = []any{}
		}
		return out, nil
	case JoinFirst:
		if len(items) == 0 {
			return nil, fmt.Errorf("join: first: the list is empty, so there is no first item")
		}
		return items[0], nil
	}
	return nil, fmt.Errorf("unknown join %q", mode)
}

// itemText renders one item for JoinLines: a string as itself, anything
// else as compact JSON. Rendering a map with %v would produce Go syntax,
// which is not something to put in someone's Slack channel.
func itemText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

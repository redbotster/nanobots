// Package scheduler closes a real gap this build has had since its first
// commit: cmd/nanobotd/main.go's own doc comment names "the scheduler (cron
// triggers)" as explicitly not built yet. Every catalog swarm declares a
// trigger: {type: cron, expr: "...", timezone: "..."} — "every weekday
// morning, recap my inbox" — but until now nothing ever actually fired one;
// every run was a human clicking Run or invoking `nanobots run` themselves.
//
// This deliberately implements a minimal standard 5-field cron parser
// rather than taking a dependency, matching this codebase's own established
// "keep the dependency list small" rule (see internal/api/server.go's doc
// comment on not pulling in a router either). It supports exactly what the
// real catalog's own swarms use: *, exact numbers, ranges (1-5), steps
// (*/2, */30), and comma lists — verified against every trigger.expr in
// examples/swarms/*.yaml, not a generic cron spec implemented in a vacuum.
package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// field bounds, in cron's own field order: minute hour day-of-month month day-of-week
var fieldBounds = [5][2]int{
	{0, 59}, // minute
	{0, 23}, // hour
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 6},  // day of week (0 = Sunday)
}

// Schedule is one parsed cron expression — a set of allowed values for each
// of the five fields, checked independently (day-of-month and day-of-week
// are OR'd together when both are restricted, matching standard cron
// semantics; neither of this catalog's own swarms actually restricts both
// at once, so this is a correctness nicety, not something exercised by
// real usage today).
type Schedule struct {
	minute, hour, dom, month, dow map[int]bool
	domRestricted, dowRestricted  bool
}

// Parse parses a standard 5-field cron expression ("min hour dom month
// dow"). Returns an error naming exactly which field was invalid, rather
// than a generic parse failure — this runs against real, human-edited YAML,
// and a clear error is what turns a typo into a two-minute fix instead of
// a silent no-op schedule.
func Parse(expr string) (*Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields (minute hour day-of-month month day-of-week), got %d in %q", len(fields), expr)
	}
	names := [5]string{"minute", "hour", "day-of-month", "month", "day-of-week"}
	sets := make([]map[int]bool, 5)
	for i, f := range fields {
		set, err := parseField(f, fieldBounds[i][0], fieldBounds[i][1])
		if err != nil {
			return nil, fmt.Errorf("cron: %s field %q: %w", names[i], f, err)
		}
		sets[i] = set
	}
	return &Schedule{
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		domRestricted: fields[2] != "*", dowRestricted: fields[4] != "*",
	}, nil
}

// maxLookahead bounds how far into the future Next will search before
// giving up — generous enough for any real schedule (even a once-a-year
// one) while still guaranteeing termination for a pathological expression
// that can never actually match (e.g. day-of-month 31 restricted to
// February, which no calendar year has).
const maxLookahead = 4 * 366 * 24 * time.Hour

// Next returns the first minute strictly after `after` (in whatever
// time.Location after is already in — callers resolve a swarm's own
// timezone before calling, Schedule itself only matches calendar fields)
// that this schedule allows, or the zero Time if none exists within
// maxLookahead.
func (s *Schedule) Next(after time.Time) time.Time {
	loc := after.Location()
	t := after.Truncate(time.Minute).Add(time.Minute)
	deadline := after.Add(maxLookahead)
	for t.Before(deadline) {
		if s.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
		t = t.In(loc)
	}
	return time.Time{}
}

func (s *Schedule) matches(t time.Time) bool {
	if !s.minute[t.Minute()] || !s.hour[t.Hour()] || !s.month[int(t.Month())] {
		return false
	}
	domOK := s.dom[t.Day()]
	dowOK := s.dow[int(t.Weekday())]
	switch {
	case s.domRestricted && s.dowRestricted:
		return domOK || dowOK // standard cron: either field alone satisfies it
	case s.domRestricted:
		return domOK
	case s.dowRestricted:
		return dowOK
	default:
		return true
	}
}

// parseField expands one comma-separated cron field (each part a "*",
// a number, a "a-b" range, or any of those with a "/step") into the set of
// values it allows, within [lo, hi] inclusive.
func parseField(field string, lo, hi int) (map[int]bool, error) {
	set := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		base, step := part, 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			base = part[:i]
			s, err := strconv.Atoi(part[i+1:])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid step %q", part[i+1:])
			}
			step = s
		}

		start, end := lo, hi
		switch {
		case base == "*":
			// full range, already set above
		case strings.Contains(base, "-"):
			bounds := strings.SplitN(base, "-", 2)
			a, err1 := strconv.Atoi(bounds[0])
			b, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil || a > b {
				return nil, fmt.Errorf("invalid range %q", base)
			}
			start, end = a, b
		default:
			n, err := strconv.Atoi(base)
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", base)
			}
			start, end = n, n
		}
		if start < lo || end > hi {
			return nil, fmt.Errorf("value out of range [%d,%d]: %q", lo, hi, part)
		}
		for v := start; v <= end; v += step {
			set[v] = true
		}
	}
	return set, nil
}

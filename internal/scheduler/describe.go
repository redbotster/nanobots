package scheduler

import (
	"fmt"
	"sort"
	"strings"
)

// Describe turns a cron expression into a sentence a person can read.
//
// The scheduler has been firing these since it was built, and the WebUI
// showed nothing about them at all — not that a swarm was scheduled, not
// when, not when next. "0 7 * * 1-5" is precise and unreadable; "Weekdays at
// 7:00 AM" is the thing worth putting on a card.
//
// It covers the shapes the catalog actually uses and the ones a person is
// likely to write by hand. Anything more exotic falls back to the raw
// expression rather than guessing — a wrong sentence about when something
// fires is worse than an honest cron string.
func Describe(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return expr
	}
	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Anything with a step, a list of hours, or a month restriction is past
	// what this phrases reliably.
	if month != "*" || strings.ContainsAny(hour, ",/") || strings.ContainsAny(minute, ",/") {
		return everyPhrase(expr, minute, hour, dom, dow)
	}
	if dom != "*" && dow != "*" {
		return expr // dom/dow OR semantics don't phrase cleanly
	}

	timePart, ok := clockPhrase(minute, hour)
	if !ok {
		return everyPhrase(expr, minute, hour, dom, dow)
	}

	switch {
	case dow != "*":
		days, ok := dayPhrase(dow)
		if !ok {
			return expr
		}
		return days + " at " + timePart
	case dom != "*":
		day, err := parseSingleInt(dom)
		if err != nil {
			return expr
		}
		return fmt.Sprintf("Monthly on the %s at %s", ordinal(day), timePart)
	default:
		return "Daily at " + timePart
	}
}

// everyPhrase handles the interval forms — "*/15 * * * *", "0 */2 * * *",
// and the same restricted to certain days, which is what the catalog's
// inbox-autopilot actually uses ("0 */2 * * 1-5"). Gives up to the raw
// expression otherwise.
func everyPhrase(expr, minute, hour, dom, dow string) string {
	if dom != "*" {
		return expr
	}

	var interval string
	switch {
	case hour == "*" && strings.HasPrefix(minute, "*/"):
		if n, err := parseSingleInt(strings.TrimPrefix(minute, "*/")); err == nil {
			interval = "Every " + plural(n, "minute")
		}
	case minute == "0" && strings.HasPrefix(hour, "*/"):
		if n, err := parseSingleInt(strings.TrimPrefix(hour, "*/")); err == nil {
			interval = "Every " + plural(n, "hour")
		}
	case minute == "*" && hour == "*":
		interval = "Every minute"
	case minute == "0" && hour == "*":
		interval = "Hourly"
	}
	if interval == "" {
		return expr
	}
	if dow == "*" {
		return interval
	}
	days, ok := dayPhrase(dow)
	if !ok {
		return expr
	}
	// "Every 2 hours" + "Weekdays" reads best as "Every 2 hours on weekdays";
	// a single day keeps its own capitalisation ("... on Fridays").
	return interval + " on " + lowerIfGeneric(days)
}

// lowerIfGeneric lowercases the generic day phrases so they read as part of
// a sentence, leaving actual day names capitalised.
func lowerIfGeneric(days string) string {
	switch days {
	case "Weekdays", "Weekends", "Every day":
		return strings.ToLower(days)
	}
	return days
}

func clockPhrase(minute, hour string) (string, bool) {
	m, err := parseSingleInt(minute)
	if err != nil {
		return "", false
	}
	h, err := parseSingleInt(hour)
	if err != nil {
		return "", false
	}
	suffix := "AM"
	display := h
	switch {
	case h == 0:
		display = 12
	case h == 12:
		suffix = "PM"
	case h > 12:
		display, suffix = h-12, "PM"
	}
	return fmt.Sprintf("%d:%02d %s", display, m, suffix), true
}

var dayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func dayPhrase(dow string) (string, bool) {
	set, err := parseField(dow, 0, 7)
	if err != nil {
		return "", false
	}
	days := make([]int, 0, len(set))
	for d := range set {
		days = append(days, d%7) // cron allows 7 for Sunday
	}
	sort.Ints(days)
	days = dedupe(days)

	switch len(days) {
	case 0:
		return "", false
	case 7:
		return "Every day", true
	case 1:
		return dayNames[days[0]] + "s", true
	}
	if equalInts(days, []int{1, 2, 3, 4, 5}) {
		return "Weekdays", true
	}
	if equalInts(days, []int{0, 6}) {
		return "Weekends", true
	}
	names := make([]string, len(days))
	for i, d := range days {
		names[i] = dayNames[d][:3]
	}
	return strings.Join(names, ", "), true
}

func parseSingleInt(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	// Sscanf stops at the first non-digit, so reject "5x" and "1-3".
	if fmt.Sprintf("%d", n) != s {
		return 0, fmt.Errorf("not a plain integer: %q", s)
	}
	return n, nil
}

func ordinal(n int) string {
	if n%100 >= 11 && n%100 <= 13 {
		return fmt.Sprintf("%dth", n)
	}
	switch n % 10 {
	case 1:
		return fmt.Sprintf("%dst", n)
	case 2:
		return fmt.Sprintf("%dnd", n)
	case 3:
		return fmt.Sprintf("%drd", n)
	}
	return fmt.Sprintf("%dth", n)
}

func plural(n int, unit string) string {
	if n == 1 {
		return unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func dedupe(sorted []int) []int {
	out := sorted[:0]
	for i, v := range sorted {
		if i == 0 || v != sorted[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package scheduler

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string) *Schedule {
	t.Helper()
	s, err := Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	return s
}

func at(t *testing.T, layout, value string) time.Time {
	t.Helper()
	tm, err := time.Parse(layout, value)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// Every one of these expressions is a real trigger.expr from
// examples/swarms/*.yaml — not synthetic cases.
func TestParseAndNextAgainstRealCatalogExpressions(t *testing.T) {
	const layout = "2006-01-02 15:04 Mon"
	cases := []struct {
		name, expr, after, wantNext string
	}{
		{"weekday mornings (daily-email-recap)", "0 7 * * 1-5",
			"2026-03-02 06:00 Mon", "2026-03-02 07:00 Mon"},
		{"weekday mornings, already past today (daily-email-recap)", "0 7 * * 1-5",
			"2026-03-02 08:00 Mon", "2026-03-03 07:00 Tue"},
		{"friday afternoon (bookkeeping-assistant)", "0 15 * * 5",
			"2026-03-02 06:00 Mon", "2026-03-06 15:00 Fri"},
		{"weekly monday (content-engine)", "0 9 * * 1",
			"2026-03-02 06:00 Mon", "2026-03-02 09:00 Mon"},
		{"every 30 minutes (support-desk-lite)", "*/30 * * * *",
			"2026-03-02 06:05 Mon", "2026-03-02 06:30 Mon"},
		{"every 2 hours on weekdays (inbox-autopilot)", "0 */2 * * 1-5",
			"2026-03-02 06:59 Mon", "2026-03-02 08:00 Mon"},
		{"skips the weekend (daily-inbox-recap)", "0 7 * * 1-5",
			"2026-03-06 08:00 Fri", "2026-03-09 07:00 Mon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustParse(t, tc.expr)
			got := s.Next(at(t, layout, tc.after))
			want := at(t, layout, tc.wantNext)
			if !got.Equal(want) {
				t.Errorf("Next(%s) = %s, want %s", tc.after, got.Format(layout), want.Format(layout))
			}
		})
	}
}

func TestParseRejectsWrongFieldCount(t *testing.T) {
	if _, err := Parse("0 7 * *"); err == nil {
		t.Fatal("expected an error for a 4-field expression")
	}
}

func TestParseRejectsOutOfRangeValue(t *testing.T) {
	if _, err := Parse("0 25 * * *"); err == nil {
		t.Fatal("expected an error for hour=25")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse("nonsense * * * *"); err == nil {
		t.Fatal("expected an error for a non-numeric field")
	}
}

func TestParseAcceptsCommaList(t *testing.T) {
	s := mustParse(t, "0 9,17 * * *")
	if !s.hour[9] || !s.hour[17] || s.hour[10] {
		t.Errorf("hour set = %v", s.hour)
	}
}

func TestNextReturnsZeroForAnImpossibleSchedule(t *testing.T) {
	// February never has a 31st — this can never fire.
	s := mustParse(t, "0 0 31 2 *")
	got := s.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !got.IsZero() {
		t.Errorf("Next = %v, want zero time for an impossible schedule", got)
	}
}

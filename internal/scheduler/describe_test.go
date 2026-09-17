package scheduler

import "testing"

func TestDescribe(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		// Every expression the real catalog uses.
		{"0 7 * * 1-5", "Weekdays at 7:00 AM"},
		{"0 8 * * 1-5", "Weekdays at 8:00 AM"},
		{"0 16 * * 1-5", "Weekdays at 4:00 PM"},
		{"0 15 * * 5", "Fridays at 3:00 PM"},
		{"0 9 * * 1", "Mondays at 9:00 AM"},

		// Clock edges, where a naive 12-hour conversion goes wrong.
		{"0 0 * * *", "Daily at 12:00 AM"},
		{"0 12 * * *", "Daily at 12:00 PM"},
		{"30 13 * * *", "Daily at 1:30 PM"},
		{"5 0 * * *", "Daily at 12:05 AM"},

		// Day sets.
		{"0 9 * * 0,6", "Weekends at 9:00 AM"},
		{"0 9 * * 1,3,5", "Mon, Wed, Fri at 9:00 AM"},
		{"0 9 * * 0-6", "Every day at 9:00 AM"},
		{"0 9 * * 7", "Sundays at 9:00 AM"}, // cron allows 7 for Sunday

		// Hourly at an offset. meeting-to-action and repurpose-everything
		// poll on the hour at :15 and :45, and both printed raw cron on
		// their card before this — to the person who was promised they
		// would never see YAML.
		{"15 * * * *", "Hourly at :15"},
		{"45 * * * *", "Hourly at :45"},
		{"5 * * * *", "Hourly at :05"},
		{"0 * * * *", "Hourly"},

		// Intervals.
		{"*/15 * * * *", "Every 15 minutes"},
		{"*/1 * * * *", "Every minute"},
		{"0 */2 * * *", "Every 2 hours"},
		{"0 */1 * * *", "Every hour"},
		{"* * * * *", "Every minute"},
		{"0 * * * *", "Hourly"},

		// Day of month.
		{"0 9 1 * *", "Monthly on the 1st at 9:00 AM"},
		{"0 9 2 * *", "Monthly on the 2nd at 9:00 AM"},
		{"0 9 3 * *", "Monthly on the 3rd at 9:00 AM"},
		{"0 9 11 * *", "Monthly on the 11th at 9:00 AM"},
		{"0 9 21 * *", "Monthly on the 21st at 9:00 AM"},

		// Beyond what this phrases: fall back to the expression rather than
		// inventing a sentence. A wrong claim about when something fires is
		// worse than an honest cron string.
		{"0 9 1 1 *", "0 9 1 1 *"},       // month restriction
		{"0 9 1 * 1", "0 9 1 * 1"},       // dom AND dow (OR semantics)
		{"0 9,17 * * *", "0 9,17 * * *"}, // hour list
		{"not a cron", "not a cron"},     // garbage
		{"0 9 * *", "0 9 * *"},           // too few fields
	} {
		t.Run(tc.expr, func(t *testing.T) {
			if got := Describe(tc.expr); got != tc.want {
				t.Errorf("Describe(%q) = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// Whatever Describe says, the schedule it describes must be one Parse
// accepts — otherwise the UI would show a friendly sentence for an
// expression the scheduler silently never fires.
func TestDescribedExpressionsAreAlsoParseable(t *testing.T) {
	for _, expr := range []string{
		"0 7 * * 1-5", "0 15 * * 5", "*/15 * * * *", "0 */2 * * *", "0 9 1 * *",
	} {
		if _, err := Parse(expr); err != nil {
			t.Errorf("Parse(%q) failed but Describe phrases it as %q: %v", expr, Describe(expr), err)
		}
	}
}

// inbox-autopilot's real trigger, plus the shapes around it. An interval
// restricted to certain days is common enough to be worth phrasing rather
// than showing someone a cron string on a card.
func TestDescribeIntervalsOnCertainDays(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"0 */2 * * 1-5", "Every 2 hours on weekdays"},
		{"*/30 * * * 1-5", "Every 30 minutes on weekdays"},
		{"0 * * * 0,6", "Hourly on weekends"},
		{"0 */4 * * 5", "Every 4 hours on Fridays"},
		{"0 */2 * * 1,3", "Every 2 hours on Mon, Wed"},
		{"* * * * 1-5", "Every minute on weekdays"},
		// Still refuses to phrase what it can't phrase.
		{"0 */2 5 * *", "0 */2 5 * *"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			if got := Describe(tc.expr); got != tc.want {
				t.Errorf("Describe(%q) = %q, want %q", tc.expr, got, tc.want)
			}
			if _, err := Parse(tc.expr); err != nil && tc.want != tc.expr {
				t.Errorf("phrased %q as %q but Parse rejects it: %v", tc.expr, tc.want, err)
			}
		})
	}
}

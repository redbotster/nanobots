package planner

import (
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func swarmWithSnaps(snaps ...schema.Snap) *schema.Nanoswarm {
	return &schema.Nanoswarm{Spec: schema.NanoswarmSpec{Snaps: snaps}}
}

func TestFanOutFor(t *testing.T) {
	t.Run("no marker means no fan-out", func(t *testing.T) {
		sw := swarmWithSnaps(schema.Snap{From: "chaser.draft_ids.0", To: "sender.draft_id"})
		fo, err := FanOutFor(sw, "sender")
		if err != nil || fo != nil {
			t.Fatalf("FanOutFor = %+v, %v; want nil, nil", fo, err)
		}
	})

	t.Run("collects every marker snap into the same bot", func(t *testing.T) {
		sw := swarmWithSnaps(
			schema.Snap{From: "chaser.draft_ids.*", To: "sender.draft_id"},
			schema.Snap{From: "chaser.overdue.*.customer_email", To: "sender.summary"},
			schema.Snap{From: "config.tone", To: "sender.tone"}, // not fanned
		)
		fo, err := FanOutFor(sw, "sender")
		if err != nil {
			t.Fatal(err)
		}
		if fo == nil || len(fo.Snaps) != 2 {
			t.Fatalf("want the 2 marker snaps, got %+v", fo)
		}
	})

	t.Run("a marker on the to side is rejected", func(t *testing.T) {
		sw := swarmWithSnaps(schema.Snap{From: "chaser.overdue.*", To: "sender.items.*"})
		if _, err := FanOutFor(sw, "sender"); err == nil {
			t.Error("expected an error: the marker says which list to iterate, not where to put each item")
		}
	})

	t.Run("only the named bot is considered", func(t *testing.T) {
		sw := swarmWithSnaps(schema.Snap{From: "chaser.overdue.*", To: "sender.invoice"})
		if IsFannedOut(sw, "notifier") {
			t.Error("a different bot's fan-out leaked")
		}
		if !IsFannedOut(sw, "sender") {
			t.Error("sender should be fanned out")
		}
	})
}

func TestHasFanOutMarker(t *testing.T) {
	for ref, want := range map[string]bool{
		"chaser.overdue.*":                true,
		"chaser.overdue.*.customer_email": true,
		"chaser.overdue.0":                false,
		"chaser.overdue":                  false,
		"chaser":                          false, // not a valid endpoint at all
	} {
		if got := HasFanOutMarker(ref); got != want {
			t.Errorf("HasFanOutMarker(%q) = %v, want %v", ref, got, want)
		}
	}
}

package planner

import (
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func TestTypeCheckSnapsIndexesIntoLists(t *testing.T) {
	drafter := &schema.Nanobot{
		Spec: schema.NanobotSpec{
			Ports: schema.Ports{Outputs: []schema.OutputPort{
				{Name: "draft_ids", Type: "list<string>"},
			}},
		},
	}
	sender := &schema.Nanobot{
		Spec: schema.NanobotSpec{
			Ports: schema.Ports{Inputs: []schema.InputPort{
				{Name: "draft_id", Type: "string"},
			}},
		},
	}
	rs := &ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{
			Snaps: []schema.Snap{{From: "drafter.draft_ids.0", To: "sender.draft_id"}},
		}},
		Bots: map[string]*ResolvedBot{
			"drafter": {Nanobot: drafter},
			"sender":  {Nanobot: sender},
		},
	}
	checks := TypeCheckSnaps(rs)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if !checks[0].OK {
		t.Fatalf("expected the list-index snap to type-check, got: %v", checks[0].Err)
	}
	if checks[0].FromType.String() != "string" {
		t.Errorf("resolved FromType = %s, want string", checks[0].FromType)
	}
}

func TestTypeCheckSnapsRejectsNonNumericFieldOnList(t *testing.T) {
	drafter := &schema.Nanobot{
		Spec: schema.NanobotSpec{
			Ports: schema.Ports{Outputs: []schema.OutputPort{
				{Name: "draft_ids", Type: "list<string>"},
			}},
		},
	}
	sender := &schema.Nanobot{
		Spec: schema.NanobotSpec{
			Ports: schema.Ports{Inputs: []schema.InputPort{{Name: "draft_id", Type: "string"}}},
		},
	}
	rs := &ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{
			Snaps: []schema.Snap{{From: "drafter.draft_ids.first", To: "sender.draft_id"}},
		}},
		Bots: map[string]*ResolvedBot{"drafter": {Nanobot: drafter}, "sender": {Nanobot: sender}},
	}
	checks := TypeCheckSnaps(rs)
	if checks[0].OK {
		t.Fatal("expected a non-numeric field on a list port to fail type-checking")
	}
}

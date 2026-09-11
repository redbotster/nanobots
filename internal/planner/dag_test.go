package planner

import (
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func TestBuildDAGDedupesMultipleSnapsBetweenSameBots(t *testing.T) {
	rs := &ResolvedSwarm{
		Swarm: &schema.Nanoswarm{Spec: schema.NanoswarmSpec{
			Bots: []schema.BotRef{{ID: "replies"}, {ID: "sender"}},
			Snaps: []schema.Snap{
				{From: "replies.draft_ids.0", To: "sender.draft_id"},
				{From: "replies.drafts.0.subject", To: "sender.summary"},
			},
		}},
		Bots: map[string]*ResolvedBot{
			"replies": {Ref: schema.BotRef{ID: "replies"}},
			"sender":  {Ref: schema.BotRef{ID: "sender"}},
		},
	}
	d, err := BuildDAG(rs)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	if got := d.Edges["replies"]; len(got) != 1 || got[0] != "sender" {
		t.Errorf("Edges[replies] = %v, want exactly one edge to sender", got)
	}
}

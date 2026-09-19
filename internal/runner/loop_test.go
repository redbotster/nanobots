package runner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// swarmWithLoop is swarmWith for a single looping bot instance.
func swarmWithLoop(loop *schema.Loop, explicitInputs map[string]any) *planner.ResolvedSwarm {
	ref := schema.BotRef{ID: "fetch", Loop: loop, Inputs: explicitInputs}
	return swarmWith([]schema.BotRef{ref}, nil)
}

// Pagination, verified through the real runBotLoop: each call's
// next_page_token feeds the next call's page_token, and the loop stops as
// soon as the token comes back empty — well before its max, and downstream
// sees the last call's outputs, not a list of all of them.
func TestALoopFeedsItsOwnOutputForward(t *testing.T) {
	var seenTokens []string
	calls := 0
	o := &Orchestrator{runBotOnceFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, rb *planner.ResolvedBot, _ fanIndex, _ int, _ step.Approver) (map[string]any, error) {
		calls++
		token, _ := rb.Ref.Inputs["page_token"].(string)
		seenTokens = append(seenTokens, token)
		switch token {
		case "":
			return map[string]any{"next_page_token": "p2", "items": []any{"a"}}, nil
		case "p2":
			return map[string]any{"next_page_token": "p3", "items": []any{"b"}}, nil
		default:
			return map[string]any{"next_page_token": "", "items": []any{"c"}}, nil
		}
	}}
	loop := &schema.Loop{
		Max:   5,
		Feed:  map[string]string{"page_token": "next_page_token"},
		Until: `{{outputs.next_page_token}} == ""`,
	}
	rs := swarmWithLoop(loop, map[string]any{"page_token": ""})

	out, err := o.runBotLoop(NewRun("probe"), rs, "fetch", rs.Bots["fetch"], loop)
	if err != nil {
		t.Fatalf("runBotLoop: %v", err)
	}
	if calls != 3 {
		t.Fatalf("ran %d iterations, want 3 (stops once next_page_token is empty)", calls)
	}
	if want := []string{"", "p2", "p3"}; !equalStrings(seenTokens, want) {
		t.Errorf("page_token sequence = %v, want %v", seenTokens, want)
	}
	if out["next_page_token"] != "" {
		t.Errorf("final output next_page_token = %v, want empty", out["next_page_token"])
	}
	if out["items"].([]any)[0] != "c" {
		t.Errorf("downstream got %v, want only the last call's items — a loop isn't a fan-out", out)
	}
}

// No until: means loop exactly max times, no more and no less.
func TestALoopStopsAtItsMaxWithoutUntil(t *testing.T) {
	calls := 0
	o := &Orchestrator{runBotOnceFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot, _ fanIndex, _ int, _ step.Approver) (map[string]any, error) {
		calls++
		return map[string]any{"next_page_token": fmt.Sprintf("page-%d", calls)}, nil
	}}
	loop := &schema.Loop{Max: 3, Feed: map[string]string{"page_token": "next_page_token"}}
	rs := swarmWithLoop(loop, map[string]any{"page_token": "start"})

	out, err := o.runBotLoop(NewRun("probe"), rs, "fetch", rs.Bots["fetch"], loop)
	if err != nil {
		t.Fatalf("runBotLoop: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (loop.max, with no until: to stop it earlier)", calls)
	}
	if out["next_page_token"] != "page-3" {
		t.Errorf("final output = %v, want the third call's output", out)
	}
}

// A fixed input the loop doesn't feed stays fixed across every iteration —
// only the fed port changes.
func TestALoopLeavesUnfedInputsAlone(t *testing.T) {
	var seenURLs []string
	o := &Orchestrator{runBotOnceFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, rb *planner.ResolvedBot, _ fanIndex, _ int, _ step.Approver) (map[string]any, error) {
		url, _ := rb.Ref.Inputs["url"].(string)
		seenURLs = append(seenURLs, url)
		return map[string]any{"next_page_token": "x"}, nil
	}}
	loop := &schema.Loop{Max: 3, Feed: map[string]string{"page_token": "next_page_token"}}
	rs := swarmWithLoop(loop, map[string]any{"url": "https://example.com/items", "page_token": ""})

	if _, err := o.runBotLoop(NewRun("probe"), rs, "fetch", rs.Bots["fetch"], loop); err != nil {
		t.Fatalf("runBotLoop: %v", err)
	}
	for i, u := range seenURLs {
		if u != "https://example.com/items" {
			t.Errorf("iteration %d saw url=%q, want it unchanged", i, u)
		}
	}
}

// A failure mid-loop is the loop's failure — no partial success, no silent
// stop, same as any other bot failing.
func TestALoopThatFailsMidwayReturnsTheFailure(t *testing.T) {
	calls := 0
	o := &Orchestrator{runBotOnceFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, _ *planner.ResolvedBot, _ fanIndex, _ int, _ step.Approver) (map[string]any, error) {
		calls++
		if calls == 2 {
			return nil, fmt.Errorf("connection reset")
		}
		return map[string]any{"next_page_token": "x"}, nil
	}}
	loop := &schema.Loop{Max: 5, Feed: map[string]string{"page_token": "next_page_token"}}
	rs := swarmWithLoop(loop, map[string]any{"page_token": ""})

	_, err := o.runBotLoop(NewRun("probe"), rs, "fetch", rs.Bots["fetch"], loop)
	if err == nil {
		t.Fatal("expected the second iteration's failure to surface")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error = %v, lost the underlying reason", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (stops at the failure, doesn't run the rest of max)", calls)
	}
}

package runner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

// swarmWithFallback is swarmWith plus a fallback: bot on the given instance
// — swarmWith itself doesn't carry Fallbacks, and it's shared by enough
// other tests that widening its signature isn't worth it for one case.
func swarmWithFallback(bots []schema.BotRef, fallbackFor, fallbackName string) *planner.ResolvedSwarm {
	rs := swarmWith(bots, nil)
	rs.Fallbacks = map[string]*planner.ResolvedBot{
		fallbackFor: {
			Ref: findRef(bots, fallbackFor),
			Nanobot: &schema.Nanobot{
				Metadata: schema.Metadata{Name: fallbackName, Version: "0.1.0"},
			},
		},
	}
	return rs
}

func findRef(bots []schema.BotRef, id string) schema.BotRef {
	for _, b := range bots {
		if b.ID == id {
			return b
		}
	}
	return schema.BotRef{}
}

// A live service call degrading to a fixture, verified: once retries are
// exhausted, the fallback bot runs in the failed one's place and the run
// still succeeds.
func TestABotFallsBackAfterRetriesExhausted(t *testing.T) {
	var ran []string
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, id string, rb *planner.ResolvedBot) error {
		ran = append(ran, rb.Nanobot.Metadata.Name)
		if rb.Nanobot.Metadata.Name == "live-fetch" {
			return fmt.Errorf("connection reset")
		}
		return nil
	}}
	rs := swarmWithFallback([]schema.BotRef{
		{ID: "fetch", Retry: 1, Fallback: "fixture-fetch@0.1.0"},
	}, "fetch", "fixture-fetch")
	// runBotFn is looked up by bot id, keyed off rs.Bots — give the primary
	// its own Nanobot name so the seam above can tell them apart.
	rs.Bots["fetch"].Nanobot.Metadata.Name = "live-fetch"

	run := NewRun("probe")
	if err := o.runLevels(run, rs, [][]string{{"fetch"}}); err != nil {
		t.Fatalf("a working fallback still failed the run: %v", err)
	}
	if want := []string{"live-fetch", "live-fetch", "fixture-fetch"}; !equalStrings(ran, want) {
		t.Errorf("ran = %v, want %v (2 tries then the fallback)", ran, want)
	}

	var sawFallingBack, sawRecovered bool
	for _, l := range run.LogEntries() {
		if strings.Contains(l.Msg, "falling back to fixture-fetch") {
			sawFallingBack = true
		}
		if strings.Contains(l.Msg, "recovered using fallback fixture-fetch") {
			sawRecovered = true
		}
	}
	if !sawFallingBack || !sawRecovered {
		t.Errorf("run log didn't say so: falling back=%v recovered=%v", sawFallingBack, sawRecovered)
	}
}

// A fallback that also fails is an honest failure, naming both bots that
// were tried rather than just the last one.
func TestAFallbackThatAlsoFailsStillFailsTheRun(t *testing.T) {
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, rb *planner.ResolvedBot) error {
		return fmt.Errorf("%s: connection reset", rb.Nanobot.Metadata.Name)
	}}
	rs := swarmWithFallback([]schema.BotRef{
		{ID: "fetch", Fallback: "fixture-fetch@0.1.0"},
	}, "fetch", "fixture-fetch")
	rs.Bots["fetch"].Nanobot.Metadata.Name = "live-fetch"

	err := o.runLevels(NewRun("probe"), rs, [][]string{{"fetch"}})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "live-fetch") || !strings.Contains(err.Error(), "fixture-fetch") {
		t.Errorf("error doesn't name both bots tried: %v", err)
	}
}

// Asking again until someone says yes is not a retry, and handing the
// decision to a fallback bot instead is the same mistake wearing a
// different hat.
func TestADeclinedApprovalIsNeverFedToFallback(t *testing.T) {
	var fallbackRan bool
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, rb *planner.ResolvedBot) error {
		if rb.Nanobot.Metadata.Name == "fixture-fetch" {
			fallbackRan = true
		}
		return fmt.Errorf(`step "gate": not approved (decided_by=cli)`)
	}}
	rs := swarmWithFallback([]schema.BotRef{
		{ID: "fetch", Fallback: "fixture-fetch@0.1.0"},
	}, "fetch", "fixture-fetch")

	if err := o.runLevels(NewRun("probe"), rs, [][]string{{"fetch"}}); err == nil {
		t.Fatal("expected a failure")
	}
	if fallbackRan {
		t.Error("a declined approval ran the fallback bot — a decision isn't a fault a substitute can fix")
	}
}

// A run stopped mid-attempt must stay stopped, not launch a whole new bot
// in the one it cut short's place.
func TestAStoppedRunIsNeverFedToFallback(t *testing.T) {
	var fallbackRan bool
	run := NewRun("probe")
	o := &Orchestrator{runBotFn: func(_ *Run, _ *planner.ResolvedSwarm, _ string, rb *planner.ResolvedBot) error {
		if rb.Nanobot.Metadata.Name == "fixture-fetch" {
			fallbackRan = true
		}
		run.Stop()
		return ErrStopped
	}}
	rs := swarmWithFallback([]schema.BotRef{
		{ID: "fetch", Fallback: "fixture-fetch@0.1.0"},
	}, "fetch", "fixture-fetch")

	_ = o.runLevels(run, rs, [][]string{{"fetch"}})
	if fallbackRan {
		t.Error("a stopped run ran the fallback bot instead of respecting the stop")
	}
}

func equalStrings(a, b []string) bool {
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

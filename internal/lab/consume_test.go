package lab

import (
	"fmt"
	"testing"

	"github.com/redbotster/nanobots/internal/foundry"
)

// Shaped from a real run's actual event stream (docs/team.md's own
// transcript): Gemini answers in three delta chunks, each its own "text"
// event. The first version of consumeDelegation kept only the last one —
// found by watching this happen in a real browser, not by a test, since
// nothing before this exercised more than a single text event.
func TestConsumeDelegationAccumulatesMultipleTextDeltas(t *testing.T) {
	// Unbuffered, and done is only sent after every event — the same
	// ordering guarantee RunDockerAgent's real, synchronous sends give
	// consumeDelegation (see its own doc comment): done cannot fire before
	// every event this run produced has already been received.
	events := make(chan foundry.Event)
	done := make(chan error, 1)
	go func() {
		events <- foundry.Event{Phase: "tool", Msg: "read_file docs/anatomy.md"}
		events <- foundry.Event{Phase: "text", Msg: "According to the document, a nanoswarm is a YAML-defined automation"}
		events <- foundry.Event{Phase: "text", Msg: " workflow that specifies a set of nanobots to run"}
		events <- foundry.Event{Phase: "text", Msg: " and wires their outputs to inputs using snaps."}
		done <- nil
	}()

	var logged []string
	summary, err := consumeDelegation("designer", events, done, func(bot, phase, msg string) {
		logged = append(logged, fmt.Sprintf("%s/%s: %s", bot, phase, msg))
	})
	if err != nil {
		t.Fatalf("consumeDelegation: %v", err)
	}
	want := "According to the document, a nanoswarm is a YAML-defined automation" +
		" workflow that specifies a set of nanobots to run" +
		" and wires their outputs to inputs using snaps."
	if summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}
	if len(logged) != 4 {
		t.Errorf("logged %d events, want 4 (every event, not just the text ones)", len(logged))
	}
}

func TestConsumeDelegationWithNoTextEventsSaysDoneAnyway(t *testing.T) {
	events := make(chan foundry.Event)
	done := make(chan error, 1)
	go func() {
		events <- foundry.Event{Phase: "tool", Msg: "read_file x.md"}
		done <- nil
	}()

	summary, err := consumeDelegation("designer", events, done, func(string, string, string) {})
	if err != nil {
		t.Fatalf("consumeDelegation: %v", err)
	}
	if summary != "done, with no final message" {
		t.Errorf("summary = %q", summary)
	}
}

func TestConsumeDelegationPropagatesAFailure(t *testing.T) {
	events := make(chan foundry.Event, 8)
	done := make(chan error, 1)
	done <- fmt.Errorf("agent container exited with an error")

	_, err := consumeDelegation("designer", events, done, func(string, string, string) {})
	if err == nil {
		t.Fatal("expected the underlying error to propagate")
	}
}

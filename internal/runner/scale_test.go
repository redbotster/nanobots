package runner

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// realisticLogLine is close to what an actual run's log entries look like —
// see any `nanobots run` transcript in the docs, e.g. "ai.generate via
// prompt_file ./prompts/triage.md -> ok (demo data — not your real
// google)". Calibrated from a real transcript, not invented, so a scale
// test using it says something about the real workload rather than an
// arbitrary stress case.
const realisticLogLine = "ai.generate via prompt_file ./prompts/triage.md -> ok (demo data — not your real google)"

// checkBound enforces bound only in a normal build. `go test -race`, which
// CLAUDE.md's verification pass always runs, was tried with a widened but
// still-fixed multiplier first — and that failed too, caught live: the
// same test measured 692ms in one -race run and 2.04s in the next, a 3x
// swing with nothing in this repo's code changing between them. That's the
// race detector's own instrumentation contending with however many other
// packages `go test ./...` happens to be running concurrently on this
// machine at that moment — inherent noise, not a number any fixed margin
// can absorb reliably. The actual performance bar is already checked by
// the plain, non-race `go test ./...` CLAUDE.md also always runs; under
// `-race` this logs the measurement (so a real regression is still visible
// by eye) without failing the build over timing that was never trying to
// measure this code in the first place.
func checkBound(t *testing.T, elapsed, bound time.Duration, msg string, args ...any) {
	t.Helper()
	if raceEnabled {
		t.Logf("(not enforced under -race, see checkBound's doc comment) "+msg, args...)
		return
	}
	if elapsed > bound {
		t.Errorf(msg, args...)
	}
}

// The measured reason this phase moved off one-JSON-file-per-run: with
// realistically-sized runs (not the near-empty ones a unit test usually
// writes), loading 1,000 of them from that format took ~59ms at startup —
// already comfortably under the 200ms bar in docs/run-history.md. This
// proves SQLite still is, not that JSON wasn't — see
// TestReloadingAnAtypicallyLogHeavyHistoryIsStillFasterThanJSONWas for the
// case that actually motivated the move.
func TestLoadingOneThousandRunsIsFastEnough(t *testing.T) {
	dir := t.TempDir()
	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for i := 0; i < MaxPersistedRuns; i++ {
		r := NewRun("morning-brief")
		store.Add(r)
		for b, step := range []string{"triage", "label", "note_urgency", "fetch_events", "fetch_mail", "brief", "render"} {
			r.Log(fmt.Sprintf("bot%d", b), step, "%s", realisticLogLine)
		}
		r.SetBotOutputs("triage", map[string]any{"urgent_count": 3, "headline": "3 urgent messages today"})
		r.SetStatus(StatusSucceeded)
	}

	start := time.Now()
	reloaded, err := newTestStore(t, dir)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if n := len(reloaded.List()); n != MaxPersistedRuns {
		t.Fatalf("loaded %d runs, want %d", n, MaxPersistedRuns)
	}
	checkBound(t, elapsed, 200*time.Millisecond, "reloading %d realistic runs took %s, want under 200ms (docs/run-history.md's acceptance bar)",
		MaxPersistedRuns, elapsed)
	t.Logf("reloaded %d realistically-sized runs in %s", MaxPersistedRuns, elapsed)
}

// Not every run is this small. A run log grows with what a run actually
// does — a bot that generates a long draft, a fixture capture, a chatty
// retry loop — and the point of moving off JSON was that the tail of that
// distribution used to cost a full second of daemon startup. This is
// deliberately an atypical, heavy case (150x the log volume of the test
// above), so it asserts what's actually provable about it: meaningfully
// faster than JSON's own measured cost at the same size (~1.02s), not a
// fixed millisecond number that would make this test flaky on a slow CI
// runner for a workload nobody's history actually looks like.
func TestReloadingAnAtypicallyLogHeavyHistoryIsStillFasterThanJSONWas(t *testing.T) {
	dir := t.TempDir()
	bigMsg := ""
	for i := 0; i < 200; i++ {
		bigMsg += "a reasonably long log line with some detail in it, "
	}
	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for i := 0; i < MaxPersistedRuns; i++ {
		r := NewRun("swarm-x")
		r.ID = uuid.NewString()
		store.Add(r)
		for b := 0; b < 15; b++ {
			r.Log(fmt.Sprintf("bot%d", b), "step", "%s", bigMsg)
		}
		r.SetBotOutputs("bot0", map[string]any{"result": map[string]any{"text": bigMsg, "n": 42}})
		r.SetStatus(StatusSucceeded)
	}

	start := time.Now()
	reloaded, err := newTestStore(t, dir)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if n := len(reloaded.List()); n != MaxPersistedRuns {
		t.Fatalf("loaded %d runs, want %d", n, MaxPersistedRuns)
	}
	// JSON's own measured cost at this exact size was ~1.02s (see
	// docs/run-history.md). A generous margin under that, not a race
	// against the clock: the point is "meaningfully better," not "exactly
	// this many milliseconds."
	const jsonBaseline = 700 * time.Millisecond
	checkBound(t, elapsed, jsonBaseline, "reloading %d heavy runs took %s, want comfortably under JSON's ~1.02s baseline", MaxPersistedRuns, elapsed)
	t.Logf("reloaded %d heavy runs (15x10KB log lines + a 10KB output each) in %s (JSON: ~1.02s)", MaxPersistedRuns, elapsed)
}

// What actually answers GET /api/runs, every 2s, from every open tab: the
// in-memory map RunStore.List() already returns, not a fresh disk read.
// This is the number that has to stay small regardless of how many runs
// are in history, and it does — because history is only ever read from
// disk once, at startup.
func TestListingRunsFromMemoryIsNotWhereTheCostIs(t *testing.T) {
	dir := t.TempDir()
	store, err := newTestStore(t, dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for i := 0; i < MaxPersistedRuns; i++ {
		r := NewRun("swarm-x")
		store.Add(r)
		r.SetStatus(StatusSucceeded)
	}

	start := time.Now()
	list := store.List()
	elapsed := time.Since(start)
	if len(list) != MaxPersistedRuns {
		t.Fatalf("List() returned %d, want %d", len(list), MaxPersistedRuns)
	}
	checkBound(t, elapsed, 10*time.Millisecond, "List() over %d in-memory runs took %s, want well under 10ms", MaxPersistedRuns, elapsed)
}

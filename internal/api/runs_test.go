package api

import (
	"testing"

	"github.com/redbotster/nanobots/internal/runner"
)

// The gap a live UI audit found: get-paid's reminders went out, its notify
// step silently didn't, and the run listed on GET /api/runs (and the SSE
// feed built from this same function) as a plain "succeeded" — identical to
// a run where nothing was skipped. tolerated_count is what the list now
// checks; the full failure list (bot, error, remedy) stays on GET
// /api/runs/{id}, which RunDetail's own banner already renders.
func TestRunSummaryToJSONReportsATolerateFailure(t *testing.T) {
	r := runner.NewRun("get-paid")
	r.AddTolerated("notify", "slack: not configured")
	r.SetStatus(runner.StatusSucceeded)

	out := runSummaryToJSON(r)
	if out["status"] != runner.StatusSucceeded {
		t.Errorf("status = %v, want succeeded — the run did finish", out["status"])
	}
	if out["tolerated_count"] != 1 {
		t.Errorf("tolerated_count = %v, want 1", out["tolerated_count"])
	}
}

func TestRunSummaryToJSONOmitsToleratedCountWhenThereIsNothingToSay(t *testing.T) {
	r := runner.NewRun("get-paid")
	r.SetStatus(runner.StatusSucceeded)

	out := runSummaryToJSON(r)
	if _, ok := out["tolerated_count"]; ok {
		t.Errorf("tolerated_count = %v, want the key absent for an ordinary clean run", out["tolerated_count"])
	}
}

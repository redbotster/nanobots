package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/scheduler"
)

// A paused schedule must be visible, or it is just a schedule that
// mysteriously stopped running.
func TestSwarmListReportsAPausedSchedule(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	if err := os.WriteFile(filepath.Join(srv.SwarmsDir, "desk.yaml"), []byte(
		`apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: desk
  description: probe
spec:
  trigger:
    type: cron
    expr: "*/30 * * * *"
  bots: []
  snaps: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.ScheduleBreaker = &scheduler.Breaker{MaxFailures: 2}
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 3; i++ {
		r := runner.NewRun("desk")
		r.StartedAt = base.Add(time.Duration(i) * time.Minute)
		r.SetError(errors.New("slack is not connected"))
		r.SetStatus(runner.StatusFailed)
		srv.Runs.Add(r)
	}

	rec := get(srv, "/api/swarms")
	var got []SwarmSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var desk *SwarmSummary
	for i := range got {
		if got[i].Name == "desk" {
			desk = &got[i]
		}
	}
	if desk == nil {
		t.Fatal("desk not in the listing")
	}
	if !desk.SchedulePaused {
		t.Error("schedule_paused is false after three identical failures")
	}
	if desk.FailureStreak != 3 {
		t.Errorf("failure_streak = %d, want 3", desk.FailureStreak)
	}
	if desk.StreakError != "slack is not connected" {
		t.Errorf("streak_error = %q — the card has nothing to show without it", desk.StreakError)
	}
}

// A manual swarm has no schedule to pause. Reporting one as paused would
// claim something was stopped when nothing was.
func TestAManualSwarmIsNeverReportedAsPaused(t *testing.T) {
	srv := testServerWithTempSwarms(t)
	if err := os.WriteFile(filepath.Join(srv.SwarmsDir, "byhand.yaml"), []byte(
		`apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: byhand
  description: probe
spec:
  bots: []
  snaps: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.ScheduleBreaker = &scheduler.Breaker{MaxFailures: 2}
	for i := 0; i < 5; i++ {
		r := runner.NewRun("byhand")
		r.StartedAt = time.Now().Add(time.Duration(i) * time.Minute)
		r.SetError(errors.New("boom"))
		r.SetStatus(runner.StatusFailed)
		srv.Runs.Add(r)
	}

	rec := get(srv, "/api/swarms")
	var got []SwarmSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, sum := range got {
		if sum.Name != "byhand" {
			continue
		}
		if sum.SchedulePaused {
			t.Error("a manual swarm was reported as having a paused schedule")
		}
		// The streak is still worth reporting — it is real.
		if sum.FailureStreak != 5 {
			t.Errorf("failure_streak = %d, want 5", sum.FailureStreak)
		}
	}
}

package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
)

// finished builds a run that already ended, at a chosen time.
func finished(swarm string, at time.Time, status runner.RunStatus, errMsg string) *runner.Run {
	r := runner.NewRun(swarm)
	r.StartedAt = at
	if status == runner.StatusFailed {
		r.SetError(errors.New(errMsg))
	}
	r.SetStatus(status)
	return r
}

func TestBreakerCountsTheStreakAndTripsAtTheThreshold(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	b := &Breaker{MaxFailures: 3}

	mk := func(n int) []*runner.Run {
		var runs []*runner.Run
		for i := 0; i < n; i++ {
			runs = append(runs, finished("desk", base.Add(time.Duration(i)*time.Minute),
				runner.StatusFailed, "slack is not connected"))
		}
		return runs
	}

	for _, tc := range []struct {
		name        string
		runs        []*runner.Run
		wantFail    int
		wantPaused  bool
		wantLastErr string
	}{
		{"no history at all", nil, 0, false, ""},
		{"one failure is not a pattern", mk(1), 1, false, "slack is not connected"},
		// Reported below the threshold too, so the UI can warn on the way
		// down rather than only once it has stopped.
		{"two failures, still trying", mk(2), 2, false, "slack is not connected"},
		{"three is the threshold", mk(3), 3, true, "slack is not connected"},
		{"and it stays tripped", mk(9), 9, true, "slack is not connected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := b.Check("desk", tc.runs)
			if got.Failures != tc.wantFail || got.Paused != tc.wantPaused {
				t.Errorf("failures=%d paused=%v, want %d/%v", got.Failures, got.Paused, tc.wantFail, tc.wantPaused)
			}
			if got.LastError != tc.wantLastErr {
				t.Errorf("last error = %q, want %q", got.LastError, tc.wantLastErr)
			}
		})
	}
}

// One success clears the streak. A swarm that works most mornings and fails
// on the odd one must never be paused — the breaker is for "never worked",
// not "sometimes fails".
func TestBreakerASuccessClearsTheStreak(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	b := &Breaker{MaxFailures: 3}
	runs := []*runner.Run{
		finished("desk", base.Add(1*time.Minute), runner.StatusFailed, "boom"),
		finished("desk", base.Add(2*time.Minute), runner.StatusFailed, "boom"),
		finished("desk", base.Add(3*time.Minute), runner.StatusSucceeded, ""),
		finished("desk", base.Add(4*time.Minute), runner.StatusFailed, "boom again"),
	}
	got := b.Check("desk", runs)
	if got.Failures != 1 || got.Paused {
		t.Errorf("failures=%d paused=%v — the success in the middle should have reset it", got.Failures, got.Paused)
	}
	if got.LastError != "boom again" {
		t.Errorf("last error = %q, want the most recent one", got.LastError)
	}
}

// Only this swarm's runs count. Another swarm failing all morning is not a
// reason to stop yours.
func TestBreakerIgnoresOtherSwarms(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	b := &Breaker{MaxFailures: 2}
	runs := []*runner.Run{
		finished("someone-else", base.Add(1*time.Minute), runner.StatusFailed, "boom"),
		finished("someone-else", base.Add(2*time.Minute), runner.StatusFailed, "boom"),
		finished("desk", base.Add(3*time.Minute), runner.StatusFailed, "boom"),
	}
	if got := b.Check("desk", runs); got.Paused {
		t.Errorf("paused on another swarm's failures: %+v", got)
	}
}

// A run still in flight is not evidence either way. Counting it as a
// failure would pause a schedule in the middle of a run that goes on to
// succeed.
func TestBreakerIgnoresRunsStillInFlight(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	b := &Breaker{MaxFailures: 2}
	inFlight := runner.NewRun("desk")
	inFlight.StartedAt = base.Add(3 * time.Minute)

	runs := []*runner.Run{
		finished("desk", base.Add(1*time.Minute), runner.StatusFailed, "boom"),
		inFlight,
	}
	got := b.Check("desk", runs)
	if got.Failures != 1 || got.Paused {
		t.Errorf("failures=%d paused=%v — an unfinished run was counted", got.Failures, got.Paused)
	}
}

// Resume means "try again", so only what happens afterwards counts. And it
// has to survive a restart, or every daemon restart silently re-runs a
// schedule that was deliberately stopped.
func TestBreakerResumeClearsTheStreakAndPersists(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	runs := []*runner.Run{
		finished("desk", base.Add(1*time.Minute), runner.StatusFailed, "boom"),
		finished("desk", base.Add(2*time.Minute), runner.StatusFailed, "boom"),
	}

	b := &Breaker{Dir: dir, MaxFailures: 2}
	if !b.Check("desk", runs).Paused {
		t.Fatal("should be paused before resuming")
	}
	if err := b.Resume("desk"); err != nil {
		t.Fatal(err)
	}
	if got := b.Check("desk", runs); got.Paused || got.Failures != 0 {
		t.Errorf("still paused after resume: %+v", got)
	}

	// A fresh breaker over the same directory — the restart case.
	if got := (&Breaker{Dir: dir, MaxFailures: 2}).Check("desk", runs); got.Paused {
		t.Errorf("a restart forgot the resume and re-paused: %+v", got)
	}
	if _, err := filepath.Glob(filepath.Join(dir, "schedule-resumed.json")); err != nil {
		t.Fatal(err)
	}

	// A failure *after* the resume starts a new streak.
	later := append(runs, finished("desk", time.Now().Add(time.Minute), runner.StatusFailed, "still boom"))
	if got := b.Check("desk", later); got.Failures != 1 {
		t.Errorf("failures after resume = %d, want 1", got.Failures)
	}
}

// A corrupt marker file must leave schedules paused rather than firing
// them. It can only ever fail in the safe direction.
func TestBreakerSurvivesACorruptMarkerFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(filepath.Join(dir, "schedule-resumed.json"), "{not json"); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	runs := []*runner.Run{
		finished("desk", base, runner.StatusFailed, "boom"),
		finished("desk", base.Add(time.Minute), runner.StatusFailed, "boom"),
	}
	if got := (&Breaker{Dir: dir, MaxFailures: 2}).Check("desk", runs); !got.Paused {
		t.Errorf("a corrupt marker un-paused a broken schedule: %+v", got)
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

// Declining an approval is the feature working, not the swarm breaking.
//
// support-desk-lite was paused on a real machine with declines counted into
// its streak — while the app's own remedy for that same error said "This
// wasn't a fault: the approval was declined." A swarm whose whole job is to
// ask before it sends must not lose its schedule for being told no.
func TestDecliningAnApprovalDoesNotPauseTheSchedule(t *testing.T) {
	dir := t.TempDir()
	b := &Breaker{Dir: dir, MaxFailures: 3}

	var runs []*runner.Run
	for i := 0; i < 5; i++ {
		r := runner.NewRun("support-desk-lite")
		r.StartedAt = time.Now().Add(time.Duration(i) * time.Minute)
		r.DeclinedByUser = true
		r.SetError(errors.New(`bot email-send-approved: step "gate": not approved (decided_by=you)`))
		r.SetStatus(runner.StatusFailed)
		runs = append(runs, r)
	}

	if st := b.Check("support-desk-lite", runs); st.Paused {
		t.Errorf("paused after %d declined approvals — declining is an answer, not a fault", st.Failures)
	}
}

// And a real failure still trips it, including one that happens to follow a
// decline. The exemption is for the declined run, not for the swarm.
func TestARealFailureStillPausesAfterADecline(t *testing.T) {
	dir := t.TempDir()
	b := &Breaker{Dir: dir, MaxFailures: 2}

	declined := runner.NewRun("s")
	declined.StartedAt = time.Now()
	declined.DeclinedByUser = true
	declined.SetError(errors.New("not approved (decided_by=you)"))
	declined.SetStatus(runner.StatusFailed)

	var runs []*runner.Run
	runs = append(runs, declined)
	for i := 0; i < 2; i++ {
		r := runner.NewRun("s")
		r.StartedAt = time.Now().Add(time.Duration(i+1) * time.Minute)
		r.SetError(errors.New("no connected account yet"))
		r.SetStatus(runner.StatusFailed)
		runs = append(runs, r)
	}

	st := b.Check("s", runs)
	if !st.Paused {
		t.Errorf("two genuine failures should still pause; got %d failures", st.Failures)
	}
	if st.Failures != 2 {
		t.Errorf("counted %d failures, want 2 — the decline should not be one", st.Failures)
	}
}

package scheduler

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/statedb"
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

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := statedb.Open(filepath.Join(t.TempDir(), "nanobots.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
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
	db := openTestDB(t)
	base := time.Now().Add(-time.Hour)
	runs := []*runner.Run{
		finished("desk", base.Add(1*time.Minute), runner.StatusFailed, "boom"),
		finished("desk", base.Add(2*time.Minute), runner.StatusFailed, "boom"),
	}

	b := &Breaker{DB: db, MaxFailures: 2}
	if !b.Check("desk", runs).Paused {
		t.Fatal("should be paused before resuming")
	}
	if err := b.Resume("desk"); err != nil {
		t.Fatal(err)
	}
	if got := b.Check("desk", runs); got.Paused || got.Failures != 0 {
		t.Errorf("still paused after resume: %+v", got)
	}

	// A fresh breaker over the same database — the restart case.
	if got := (&Breaker{DB: db, MaxFailures: 2}).Check("desk", runs); got.Paused {
		t.Errorf("a restart forgot the resume and re-paused: %+v", got)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schedule_resumes WHERE swarm_name = 'desk'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("schedule_resumes has %d row(s) for desk, want 1", n)
	}

	// A failure *after* the resume starts a new streak.
	later := append(runs, finished("desk", time.Now().Add(time.Minute), runner.StatusFailed, "still boom"))
	if got := b.Check("desk", later); got.Failures != 1 {
		t.Errorf("failures after resume = %d, want 1", got.Failures)
	}
}

// A row that won't parse must leave schedules paused rather than firing
// them. It can only ever fail in the safe direction — the SQLite-era
// equivalent of the old corrupt-JSON-file test: loadLocked skips a row it
// can't read rather than treating it as "never resumed" incorrectly in the
// other direction.
func TestBreakerSurvivesAnUnparseableResumeRow(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(scheduleResumesSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schedule_resumes (swarm_name, resumed_at) VALUES ('desk', 'not-a-timestamp')`); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	runs := []*runner.Run{
		finished("desk", base, runner.StatusFailed, "boom"),
		finished("desk", base.Add(time.Minute), runner.StatusFailed, "boom"),
	}
	if got := (&Breaker{DB: db, MaxFailures: 2}).Check("desk", runs); !got.Paused {
		t.Errorf("an unparseable resume row un-paused a broken schedule: %+v", got)
	}
}

// Declining an approval is the feature working, not the swarm breaking.
//
// support-desk-lite was paused on a real machine with declines counted into
// its streak — while the app's own remedy for that same error said "This
// wasn't a fault: the approval was declined." A swarm whose whole job is to
// ask before it sends must not lose its schedule for being told no.
func TestDecliningAnApprovalDoesNotPauseTheSchedule(t *testing.T) {
	db := openTestDB(t)
	b := &Breaker{DB: db, MaxFailures: 3}

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
	db := openTestDB(t)
	b := &Breaker{DB: db, MaxFailures: 2}

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

// Check's answer must not depend on being handed other swarms' runs.
//
// handleListSwarms relies on this. It used to pass the whole history to
// Check once per swarm — 18 swarms against 200 runs, each run's mutex taken
// three times, on an endpoint polled every four seconds and paying that
// even to answer 304. It now groups the runs by swarm once and passes each
// swarm only its own, which is the same answer only if Check genuinely
// discards the rest rather than, say, using the full list to order
// something.
func TestCheckIsTheSameAnswerWithOrWithoutOtherSwarmsRuns(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	mine := []*runner.Run{
		finished("desk", base.Add(1*time.Minute), runner.StatusFailed, "boom"),
		finished("desk", base.Add(3*time.Minute), runner.StatusFailed, "boom"),
		finished("desk", base.Add(5*time.Minute), runner.StatusFailed, "boom"),
	}
	// The same runs with three other swarms' runs interleaved by time, so a
	// Check that looked at position rather than name would see them.
	all := []*runner.Run{
		finished("other", base.Add(0*time.Minute), runner.StatusSucceeded, ""),
		mine[0],
		finished("other", base.Add(2*time.Minute), runner.StatusSucceeded, ""),
		mine[1],
		finished("third", base.Add(4*time.Minute), runner.StatusFailed, "unrelated"),
		mine[2],
		finished("other", base.Add(6*time.Minute), runner.StatusSucceeded, ""),
	}

	b := &Breaker{MaxFailures: 3}
	withOthers := b.Check("desk", all)
	justMine := b.Check("desk", mine)

	if withOthers.Paused != justMine.Paused ||
		withOthers.Failures != justMine.Failures ||
		withOthers.LastError != justMine.LastError {
		t.Errorf("Check disagreed depending on what else it was given:\n  all runs: %+v\n  only its own: %+v",
			withOthers, justMine)
	}
	if !justMine.Paused {
		t.Error("three failures against MaxFailures 3 did not pause — the test proves nothing")
	}
}

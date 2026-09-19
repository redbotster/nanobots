package scheduler

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/redbotster/nanobots/internal/runner"
)

// A schedule that has never once worked should stop trying.
//
// This was found by reading a real machine's run history: 198 runs, 135 of
// them failed, and one swarm — support-desk-lite, on "every 30 minutes" —
// accounted for 85 of those. Forty-one of its runs held a container open
// for the full 30-minute ceiling before being killed. Slack had never been
// connected, so every single run failed the same way, and it had been doing
// that for as long as the daemon had been up: roughly twenty hours of
// container time spent re-learning one fact.
//
// Nothing anywhere said so. The Runs page showed a wall of red with no
// indication that it was one fact repeated, the swarm's card looked like
// any other, and the scheduler cheerfully queued the next one. The failure
// mode is not that a run fails — runs fail for good reasons, and the error
// messages here are unusually clear about why. It is that failing forever
// costs the same as working, and nobody is told.
//
// So: after MaxFailures consecutive failures a schedule stops firing and
// says why, in the one place the user is already looking. Resuming is one
// click, and fixing the underlying cause (connecting Slack) is what the
// message actually asks for.
//
// Deliberately derived from run history rather than kept as its own state
// machine. The history is already persisted across restarts, so the pause
// is too — a daemon restart is not a reason to spend another twenty hours
// proving the same point. The only stored state is "the user pressed
// Resume at time T", after which failures are counted afresh.
type Breaker struct {
	// DB is where resume markers are written — the same shared SQLite file
	// run history moved into (internal/statedb), a `schedule_resumes`
	// table of its own. nil disables persistence, which is what tests get;
	// the breaker still works, it just forgets a Resume across restarts.
	DB *sql.DB
	// MaxFailures is how many consecutive failures trip it. Zero means
	// DefaultMaxFailures.
	MaxFailures int

	mu      sync.Mutex
	resumed map[string]time.Time
	loaded  bool
}

const scheduleResumesSchema = `
CREATE TABLE IF NOT EXISTS schedule_resumes (
	swarm_name TEXT PRIMARY KEY,
	resumed_at TEXT NOT NULL
);
`

// DefaultMaxFailures is high enough to ride out a transient blip — a
// provider 503, a laptop asleep at the wrong moment — and low enough that a
// genuinely broken schedule is stopped within a few cycles rather than a
// few hundred.
const DefaultMaxFailures = 5

// Status is what the breaker knows about one swarm.
type Status struct {
	// Paused is true when this schedule has stopped firing.
	Paused bool
	// Failures is the number of consecutive failed runs, counted back from
	// the most recent and stopping at the first success (or at the last
	// Resume). Reported even below the threshold, so the UI can warn before
	// it trips rather than only after.
	Failures int
	// LastError is the most recent failure's message — the thing worth
	// showing, since all of them are usually the same.
	LastError string
	// Since is when the current failing streak started.
	Since time.Time
}

func (b *Breaker) max() int {
	if b.MaxFailures > 0 {
		return b.MaxFailures
	}
	return DefaultMaxFailures
}

// Check reports the breaker's view of one swarm, given every run known.
//
// Runs still in flight are skipped rather than counted either way: a run
// that has not finished is not evidence, and treating it as a failure would
// pause a schedule mid-success.
func (b *Breaker) Check(swarmName string, runs []*runner.Run) Status {
	after := b.resumedAt(swarmName)

	mine := make([]*runner.Run, 0, 8)
	for _, r := range runs {
		if r.SwarmName != swarmName || !r.StartedAt.After(after) {
			continue
		}
		// A run you stopped by hand is not the swarm failing. Five of those
		// in a row should not pause a schedule — that would be the app
		// misreading a deliberate act as a fault.
		if r.WasStoppedByUser() {
			continue
		}
		// Nor is a run you declined. An approval gate exists to be answered
		// either way, and a swarm whose whole job is to ask before it sends
		// should not lose its schedule for being told no.
		//
		// This was live: support-desk-lite sat paused with declines counted
		// into its streak, while the app's own remedy for that same error
		// said "This wasn't a fault: the approval was declined." One build,
		// two opinions about whether the user had done something wrong.
		if r.WasDeclinedByUser() {
			continue
		}
		switch r.GetStatus() {
		case runner.StatusSucceeded, runner.StatusFailed:
			mine = append(mine, r)
		}
	}
	// Most recent first, so the streak is a prefix.
	sort.Slice(mine, func(i, j int) bool { return mine[i].StartedAt.After(mine[j].StartedAt) })

	var st Status
	for _, r := range mine {
		if r.GetStatus() != runner.StatusFailed {
			break
		}
		st.Failures++
		if st.LastError == "" {
			st.LastError = r.GetError()
		}
		st.Since = r.StartedAt
	}
	st.Paused = st.Failures >= b.max()
	return st
}

// Resume clears the streak for one swarm by recording that the user asked
// for another try, so the next Check counts only runs after this moment.
func (b *Breaker) Resume(swarmName string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	now := time.Now()
	if err := b.saveLocked(swarmName, now); err != nil {
		return err
	}
	b.resumed[swarmName] = now
	return nil
}

func (b *Breaker) resumedAt(swarmName string) time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	return b.resumed[swarmName]
}

func (b *Breaker) loadLocked() {
	if b.loaded {
		return
	}
	b.loaded = true
	b.resumed = map[string]time.Time{}
	if b.DB == nil {
		return
	}
	if _, err := b.DB.Exec(scheduleResumesSchema); err != nil {
		return
	}
	// A row that won't parse, or a table that can't be read at all, means
	// "no one has resumed anything" — the safe reading either way: it can
	// only leave a broken schedule paused, never start one firing again
	// behind the user's back.
	rows, err := b.DB.Query(`SELECT swarm_name, resumed_at FROM schedule_resumes`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name, at string
		if rows.Scan(&name, &at) != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			continue
		}
		b.resumed[name] = t
	}
}

func (b *Breaker) saveLocked(swarmName string, at time.Time) error {
	if b.DB == nil {
		return nil
	}
	if _, err := b.DB.Exec(scheduleResumesSchema); err != nil {
		return err
	}
	_, err := b.DB.Exec(`
		INSERT INTO schedule_resumes (swarm_name, resumed_at) VALUES (?, ?)
		ON CONFLICT (swarm_name) DO UPDATE SET resumed_at = excluded.resumed_at`,
		swarmName, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("recording resume for %q: %w", swarmName, err)
	}
	return nil
}

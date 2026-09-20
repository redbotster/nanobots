package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/schema"
)

// Orchestrator is the subset of *runner.Orchestrator the scheduler needs —
// an interface so a fake stands in for real Docker execution in tests, the
// same narrow-interface pattern already used throughout this codebase
// (e.g. internal/step's googleAPI, internal/foundry's Agent).
type Orchestrator interface {
	ExecuteSwarm(swarmPath string) (*runner.Run, error)
}

// RunStore is the subset of *runner.RunStore the scheduler needs. List is
// here for the breaker: deciding whether to fire means knowing how the last
// few runs of this swarm went.
type RunStore interface {
	Add(r *runner.Run)
	List() []*runner.Run
}

// pollFallbackInterval is only used if fsnotify itself can't start — see
// Run's doc comment. It matches the interval every version of this
// scheduler used before fsnotify existed.
const pollFallbackInterval = 20 * time.Second

// maxWait bounds how long Run ever sleeps between rescans, even when
// nothing is currently scheduled to fire. Not a polling interval in the
// old sense — a rescan this triggers finds nothing to do and goes back to
// sleep until the next real event or the next actual due time, whichever
// is sooner — but a ceiling so a bug in next-fire math (wrong timezone,
// wrong year) self-heals within a day instead of sleeping forever.
const maxWait = time.Hour

// Scheduler watches SwarmsDir and fires any swarm whose trigger:
// {type: cron, ...} is due — the same real execution path a human clicking
// "Run" in the WebUI goes through (Orchestrator.ExecuteSwarm +
// RunStore.Add), so a scheduled run shows up in the Runs page exactly like
// a manual one, including its own approval gates.
type Scheduler struct {
	Orchestrator Orchestrator
	Runs         RunStore
	SwarmsDir    string
	// PollInterval, when set, is only honored by the fsnotify-unavailable
	// fallback path — see Run. 0 there means pollFallbackInterval.
	PollInterval time.Duration
	Now          func() time.Time // 0 => time.Now; overridable for tests

	// Breaker stops a schedule that has failed the same way over and over.
	// nil means fire unconditionally, which is what this did before and
	// what a test gets unless it asks otherwise.
	Breaker *Breaker

	mu    sync.Mutex
	state map[string]*swarmState
}

type swarmState struct {
	expr     string
	schedule *Schedule
	loc      *time.Location
	nextFire time.Time
	// paused tracks whether we have already logged the pause, so a swarm
	// that is due every 30 seconds doesn't write a log line every 30
	// seconds saying it isn't running.
	paused bool
}

// Run blocks until ctx is cancelled — meant to be started as
// `go scheduler.Run(ctx)` alongside nanobotd's HTTP server.
//
// It watches SwarmsDir with fsnotify rather than polling it: a swarm added,
// edited, or removed while nanobotd keeps running takes effect on the next
// filesystem event, not on the next tick of a fixed timer, and the process
// otherwise sleeps until the earliest known cron fire time instead of
// waking up every 20 seconds to find nothing due. If fsnotify itself can't
// start — an OS-level watch-descriptor limit, a SwarmsDir that doesn't
// exist yet — that's a real, documented failure mode (inotify has a
// system-wide cap), not a reason to stop scheduling: it falls back to the
// plain polling loop every version of this scheduler used before, logged
// so it's visible rather than a silent degradation.
func (s *Scheduler) Run(ctx context.Context) {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	s.tick(now())

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("scheduler: fsnotify unavailable (%v) — falling back to polling every %s", err, s.pollInterval())
		s.runPolling(ctx, now)
		return
	}
	defer watcher.Close()
	if err := watcher.Add(s.SwarmsDir); err != nil {
		log.Printf("scheduler: could not watch %s (%v) — falling back to polling every %s", s.SwarmsDir, err, s.pollInterval())
		s.runPolling(ctx, now)
		return
	}

	for {
		timer := time.NewTimer(s.wait(now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case _, ok := <-watcher.Events:
			timer.Stop()
			if !ok {
				return
			}
			// Any event in the directory is worth a full rescan — cheap,
			// and it avoids guessing which specific file changed from an
			// event that can arrive as create+remove+create for a single
			// editor save (temp file, then rename).
			s.tick(now())
		case werr, ok := <-watcher.Errors:
			timer.Stop()
			if !ok {
				return
			}
			log.Printf("scheduler: watch error: %v", werr)
		case <-timer.C:
			s.tick(now())
		}
	}
}

func (s *Scheduler) pollInterval() time.Duration {
	if s.PollInterval > 0 {
		return s.PollInterval
	}
	return pollFallbackInterval
}

// runPolling is the pre-fsnotify behavior, kept as the fallback Run's doc
// comment describes rather than deleted: a fixed-interval rescan that
// notices file changes because it looks again, not because anything told
// it to.
func (s *Scheduler) runPolling(ctx context.Context, now func() time.Time) {
	ticker := time.NewTicker(s.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(now())
		}
	}
}

// wait returns how long to sleep before the next rescan is worth doing on
// its own: the time until the earliest nextFire currently tracked, bounded
// to [0, maxWait]. A negative-or-zero result (something is already due)
// fires the timer immediately rather than blocking.
func (s *Scheduler) wait(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	earliest := now.Add(maxWait)
	found := false
	for _, st := range s.state {
		if st.nextFire.IsZero() {
			continue
		}
		if st.nextFire.Before(earliest) {
			earliest = st.nextFire
			found = true
		}
	}
	if !found {
		return maxWait
	}
	if d := earliest.Sub(now); d > 0 {
		return d
	}
	return 0
}

// tick re-scans SwarmsDir (a swarm can be added, edited, or removed while
// nanobotd keeps running — this deliberately doesn't require a restart to
// notice), computes each cron-triggered swarm's next fire time, and
// executes any that are due. A single bad cron expression, or one swarm
// failing to launch, is logged and skipped rather than blocking every
// other swarm's schedule.
func (s *Scheduler) tick(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		s.state = map[string]*swarmState{}
	}

	seen := map[string]bool{}
	_ = schema.ForEachSwarmFile(s.SwarmsDir, func(path string, sw *schema.Nanoswarm) bool {
		if sw.Spec.Trigger.Type != "cron" {
			return true
		}
		seen[path] = true

		st, ok := s.state[path]
		if !ok || st.expr != sw.Spec.Trigger.Expr {
			sched, err := Parse(sw.Spec.Trigger.Expr)
			if err != nil {
				log.Printf("scheduler: %s: bad cron expression %q: %v", path, sw.Spec.Trigger.Expr, err)
				return true
			}
			loc := time.UTC
			if tz := sw.Spec.Trigger.Timezone; tz != "" {
				if l, err := time.LoadLocation(tz); err == nil {
					loc = l
				} else {
					log.Printf("scheduler: %s: unknown timezone %q, using UTC: %v", path, tz, err)
				}
			}
			st = &swarmState{expr: sw.Spec.Trigger.Expr, schedule: sched, loc: loc}
			// A newly-seen or just-edited schedule that already matches
			// this very instant is due now, not at its next occurrence —
			// otherwise editing a swarm to "every minute" (or nanobotd
			// simply starting up during a matching minute) would silently
			// wait a full cycle before ever firing.
			nowInLoc := now.In(loc)
			if sched.matches(nowInLoc) {
				st.nextFire = nowInLoc
			} else {
				st.nextFire = sched.Next(nowInLoc)
			}
			s.state[path] = st
		}

		if st.nextFire.IsZero() {
			return true
		}
		if now.In(st.loc).Before(st.nextFire) {
			return true
		}

		// A schedule that has failed the same way N times running is not
		// firing again until someone looks at it. The run history says so,
		// so this survives a restart — see breaker.go for why twenty hours
		// of identical failures is the thing being prevented.
		if s.Breaker != nil {
			if bs := s.Breaker.Check(sw.Metadata.Name, s.Runs.List()); bs.Paused {
				if !st.paused {
					st.paused = true
					log.Printf("scheduler: %s paused after %d consecutive failures — resume it from the app once fixed. Last error: %s",
						path, bs.Failures, bs.LastError)
				}
				// Keep the clock moving so "next run" stays honest and a
				// resume doesn't immediately fire a backlog.
				st.nextFire = st.schedule.Next(now.In(st.loc))
				return true
			}
			st.paused = false
		}

		log.Printf("scheduler: firing %s (due %s)", path, st.nextFire.Format(time.RFC3339))
		run, err := s.Orchestrator.ExecuteSwarm(path)
		if err != nil {
			log.Printf("scheduler: %s: failed to start: %v", path, err)
		} else {
			// Safe to set directly, no lock: nothing can observe this run
			// until Runs.Add below publishes it, and TriggeredBy is never
			// written again after this.
			run.TriggeredBy = "schedule"
			s.Runs.Add(run)
		}
		st.nextFire = st.schedule.Next(now.In(st.loc))
		return true
	})

	for path := range s.state {
		if !seen[path] {
			delete(s.state, path)
		}
	}
}

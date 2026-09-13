package scheduler

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

// Scheduler polls SwarmsDir on an interval and fires any swarm whose
// trigger: {type: cron, ...} is due — the same real execution path a
// human clicking "Run" in the WebUI goes through (Orchestrator.ExecuteSwarm
// + RunStore.Add), so a scheduled run shows up in the Runs page exactly
// like a manual one, including its own approval gates.
type Scheduler struct {
	Orchestrator Orchestrator
	Runs         RunStore
	SwarmsDir    string
	PollInterval time.Duration    // 0 => 20s
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

// Run blocks, ticking until ctx is cancelled — meant to be started as
// `go scheduler.Run(ctx)` alongside nanobotd's HTTP server.
func (s *Scheduler) Run(ctx context.Context) {
	interval := s.PollInterval
	if interval == 0 {
		interval = 20 * time.Second
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	s.tick(now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			_ = t
			s.tick(now())
		}
	}
}

// tick re-scans SwarmsDir (a swarm can be added, edited, or removed while
// nanobotd keeps running — this deliberately doesn't require a restart to
// notice), computes each cron-triggered swarm's next fire time, and
// executes any that are due. A single bad cron expression, or one swarm
// failing to launch, is logged and skipped rather than blocking every
// other swarm's schedule.
func (s *Scheduler) tick(now time.Time) {
	entries, err := os.ReadDir(s.SwarmsDir)
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		s.state = map[string]*swarmState{}
	}

	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(s.SwarmsDir, e.Name())
		sw, err := schema.LoadNanoswarm(path)
		if err != nil || sw.Spec.Trigger.Type != "cron" {
			continue
		}
		seen[path] = true

		st, ok := s.state[path]
		if !ok || st.expr != sw.Spec.Trigger.Expr {
			sched, err := Parse(sw.Spec.Trigger.Expr)
			if err != nil {
				log.Printf("scheduler: %s: bad cron expression %q: %v", path, sw.Spec.Trigger.Expr, err)
				continue
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
			continue
		}
		if now.In(st.loc).Before(st.nextFire) {
			continue
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
				continue
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
	}

	for path := range s.state {
		if !seen[path] {
			delete(s.state, path)
		}
	}
}

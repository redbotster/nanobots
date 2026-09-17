package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/runner"
	"github.com/redbotster/nanobots/internal/scheduler"
	"github.com/redbotster/nanobots/internal/schema"
)

// SwarmSummary is what the WebUI's swarm list needs to render a gallery
// entry and then load the full canvas for one — see web/src/pages/Swarms.tsx.
// ServicesLive/ServicesTotal let the gallery show "how real is this swarm
// right now" at a glance, without opening it — a swarm resolution failure
// (a broken bot ref) just leaves both at 0 rather than failing the whole
// listing. LastRun* are omitted entirely (via omitempty) when the swarm has
// never run this session — the gallery and the run history were previously
// two completely disconnected parts of the UI; a human had no way to tell
// "is this swarm actually working" without opening it and checking Runs by
// hand.
type SwarmSummary struct {
	Path           string `json:"path"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	ServicesLive   int    `json:"services_live"`
	ServicesTotal  int    `json:"services_total"`
	LastRunID      string `json:"last_run_id,omitempty"`
	LastRunStatus  string `json:"last_run_status,omitempty"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastRunTrigger string `json:"last_run_trigger,omitempty"`

	// The schedule this swarm fires on, if any. The scheduler has been
	// firing these all along while the UI said nothing about them — you
	// couldn't tell a swarm was scheduled, when it ran, or when it would
	// run next. Absent for a swarm with no cron trigger.
	Schedule     string `json:"schedule,omitempty"`      // "Weekdays at 7:00 AM"
	ScheduleExpr string `json:"schedule_expr,omitempty"` // "0 7 * * 1-5", for anyone who wants the truth
	Timezone     string `json:"timezone,omitempty"`
	NextRunAt    string `json:"next_run_at,omitempty"`
	// ScheduleError explains a cron expression the scheduler cannot parse.
	// Such a swarm never fires, and previously said so only in a daemon log
	// line nobody reads.
	ScheduleError string `json:"schedule_error,omitempty"`
	// TriggerType is the swarm's declared trigger ("cron", "event",
	// "webhook", "manual"). Reported even when nothing in this build fires
	// it, so the UI can say so — see InertTrigger.
	TriggerType string `json:"trigger_type,omitempty"`
	// InertTrigger is set when a swarm declares automation this build
	// doesn't implement, and looked identical to an unscheduled one in the
	// UI — which reads as "manual by design" rather than "its automation
	// isn't built yet".
	//
	// Only `event:` is left. Cron has always fired, and webhooks now do too
	// (internal/api/webhooks.go), so a webhook swarm reports trigger_type
	// "webhook" with no inert flag and the UI offers its URL instead of an
	// apology.
	InertTrigger string `json:"inert_trigger,omitempty"`

	// SchedulePaused is set when this swarm's schedule has stopped firing
	// because it kept failing. Before the breaker existed, a swarm whose
	// Slack was never connected would fail every 30 minutes forever, and
	// its card looked exactly like one that worked.
	SchedulePaused bool `json:"schedule_paused,omitempty"`
	// FailureStreak is how many consecutive runs have failed. Reported
	// below the pause threshold too, so a card can warn on the way down.
	FailureStreak int `json:"failure_streak,omitempty"`
	// StreakError is the most recent failure's message — the one worth
	// showing, since a streak is nearly always the same fact repeated.
	StreakError string `json:"streak_error,omitempty"`

	// NeedsApproval is set when one of this swarm's bots stops to ask a
	// human before it acts.
	//
	// Harmless on its own, and the whole point of the approval feature. It
	// matters next to a schedule: a swarm that fires at 7am and then waits
	// for a person only works if a person is there, and when nobody is, the
	// run holds a container until the bot's max_runtime_secs and dies. That
	// combination is 7 of the 16 catalog swarms, and it is the single
	// largest source of failed runs on the machine this was built on — 54
	// of them, 27 hours of container time.
	//
	// Nothing in the UI said so. A card showed "⏰ Weekdays at 7:00 AM" and
	// left you to discover the rest from a run that failed overnight.
	NeedsApproval bool `json:"needs_approval,omitempty"`
}

// describeSchedule fills in the schedule fields from a swarm's trigger,
// using the same parser the scheduler itself runs on — so what the UI shows
// and what actually fires can't disagree.
func describeSchedule(sum *SwarmSummary, t schema.Trigger, now time.Time) {
	sum.TriggerType = t.Type
	if t.Type != "cron" {
		// `event:` is the last trigger nothing fires. Say so rather than
		// rendering it identically to a swarm that has no trigger at all.
		if t.Type == "event" {
			sum.InertTrigger = t.Expr
		}
		return
	}
	if t.Expr == "" {
		sum.ScheduleError = "trigger type is cron but no expression is set"
		return
	}
	sum.ScheduleExpr = t.Expr
	sum.Timezone = t.Timezone

	sched, err := scheduler.Parse(t.Expr)
	if err != nil {
		sum.ScheduleError = err.Error()
		return
	}
	sum.Schedule = scheduler.Describe(t.Expr)

	loc := time.UTC
	if t.Timezone != "" {
		if l, lerr := time.LoadLocation(t.Timezone); lerr == nil {
			loc = l
		} else {
			sum.ScheduleError = fmt.Sprintf("unknown timezone %q, treating as UTC", t.Timezone)
		}
	}
	if next := sched.Next(now.In(loc)); !next.IsZero() {
		sum.NextRunAt = next.Format(time.RFC3339)
	}
}

// lastRunFor finds the most recently started run matching swarmName —
// matched by name, not path, since that's all a Run knows about the swarm
// that produced it (runner.Run.SwarmName comes from the swarm's own
// metadata.name, not its file path).
func lastRunFor(runs []*runner.Run, swarmName string) *runner.Run {
	var latest *runner.Run
	for _, r := range runs {
		if r.SwarmName != swarmName {
			continue
		}
		if latest == nil || r.StartedAt.After(latest.StartedAt) {
			latest = r
		}
	}
	return latest
}

// inspectSwarm resolves every bot a swarm references (the same
// planner.Resolve every real plan/save already goes through) and reports
// two things about the set: how many of their declared services are
// switched off connection: demo, and whether any of them stops to ask a
// human.
//
// Both come from one Resolve because Resolve is the expensive part, and
// asking it twice for two facts about the same bots would be the kind of
// waste this file has been trimmed for before.
func inspectSwarm(sw *schema.Nanoswarm, botsDir string) (live, total int, needsApproval bool) {
	resolved, err := planner.Resolve(sw, botsDir)
	if err != nil {
		return 0, 0, false
	}
	for _, b := range resolved.Bots {
		for _, svc := range b.Nanobot.Spec.Services {
			total++
			if svc.Connection != "" && svc.Connection != schema.ConnectionDemo {
				live++
			}
		}
		for _, st := range b.Nanobot.Spec.Steps {
			if st.Type == "approve" {
				needsApproval = true
			}
		}
	}
	return live, total, needsApproval
}

// handleListSwarms scans examples/swarms/*.yaml — there's no swarm registry
// yet (blueprint §4 #8), so "every .yaml file in this one directory" is the
// whole discovery mechanism for now.
func (s *Server) handleListSwarms(w http.ResponseWriter, r *http.Request) {
	dir := s.swarmsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var allRuns []*runner.Run
	if s.Runs != nil {
		allRuns = s.Runs.List()
	}
	// Grouped once rather than scanned per swarm.
	//
	// lastRunFor and Breaker.Check each walked the whole history looking for
	// one swarm's runs, so 18 swarms against 200 runs was 7,200 iterations —
	// every one of them taking the run's mutex three times, for GetStatus,
	// WasStoppedByUser and WasDeclinedByUser. This endpoint is polled every
	// four seconds while the swarm list is on screen, and it costs that even
	// to answer 304: the body has to be built before its hash can be
	// compared.
	bySwarm := make(map[string][]*runner.Run, len(allRuns))
	for _, r := range allRuns {
		bySwarm[r.SwarmName] = append(bySwarm[r.SwarmName], r)
	}

	var swarms []SwarmSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		sw, err := schema.LoadNanoswarm(path)
		if err != nil {
			continue
		}
		relPath, err := filepath.Rel(filepath.Dir(s.BotsDir), path)
		if err != nil {
			relPath = path
		}
		live, total, needsApproval := inspectSwarm(sw, s.BotsDir)
		summary := SwarmSummary{
			Path: relPath, Name: sw.Metadata.Name, Description: sw.Metadata.Description,
			ServicesLive: live, ServicesTotal: total, NeedsApproval: needsApproval,
		}
		describeSchedule(&summary, sw.Spec.Trigger, time.Now())
		mine := bySwarm[sw.Metadata.Name]
		if last := lastRunFor(mine, sw.Metadata.Name); last != nil {
			summary.LastRunID = last.ID
			summary.LastRunStatus = string(last.GetStatus())
			summary.LastRunAt = last.StartedAt.Format(time.RFC3339)
			summary.LastRunTrigger = last.TriggeredBy
		}
		if s.ScheduleBreaker != nil {
			// Check's first act is to discard every run belonging to another
			// swarm, so handing it the pre-filtered slice is the same answer
			// with the loop already done.
			st := s.ScheduleBreaker.Check(sw.Metadata.Name, mine)
			summary.FailureStreak = st.Failures
			summary.StreakError = st.LastError
			// Only a cron swarm has a schedule to pause. A manual swarm can
			// have a failing streak worth showing, but nothing is being
			// stopped, and saying "paused" would be a lie.
			summary.SchedulePaused = st.Paused && summary.TriggerType == "cron"
		}
		swarms = append(swarms, summary)
	}
	writeJSONCached(w, r, http.StatusOK, nonNil(swarms))
}

// handleResumeSchedule starts a paused schedule firing again.
//
// The pause is derived from run history (see internal/scheduler/breaker.go),
// so this records "the user asked for another try at time T" and failures
// are counted afresh from there. It does not fire the swarm — resuming a
// schedule and running a swarm are different intentions, and conflating
// them would mean you cannot un-pause something without also triggering it.
func (s *Server) handleResumeSchedule(w http.ResponseWriter, r *http.Request) {
	if s.ScheduleBreaker == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("this daemon has no scheduler"))
		return
	}
	name := r.PathValue("swarm")
	if _, _, err := s.swarmByName(name); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err := s.ScheduleBreaker.Resume(name); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "swarm": name})
}

// handleDescribeSchedule turns a cron expression into English, and says so
// when it isn't one.
//
// The composer writes cron now, and a cron expression the user cannot read
// is a promise they cannot check — the entire reason to show the schedule
// before saving is so they can confirm "every Friday" became Friday. This
// answers with the same scheduler.Describe the swarm list uses and the same
// scheduler.Parse that decides whether it fires.
//
// next_run_at used to be computed as sched.Next(time.Now().UTC()) while the
// scheduler computes sched.Next(now.In(loc)) — so this endpoint reported
// 07:00Z for a swarm that fires at 07:00 America/Chicago, five hours out,
// and returned the identical instant whatever timezone it was asked about.
// The comment here claimed the picker and reality "cannot drift", which is
// exactly the kind of claim that stops anyone checking. Nothing rendered
// the field yet, so nobody had been misled; it was a trap set for the next
// caller.
func (s *Server) handleDescribeSchedule(w http.ResponseWriter, r *http.Request) {
	expr := strings.TrimSpace(r.URL.Query().Get("expr"))
	if expr == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "human": "only when you press Run"})
		return
	}
	sched, err := scheduler.Parse(expr)
	if err != nil {
		// 200, not 400: "that isn't a schedule" is the answer to the
		// question, not a failure to answer it. The picker renders it as
		// you type, and a stream of 400s in the console would be noise.
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	// Same resolution order the saved swarm will get: an explicit zone, else
	// this machine's, else UTC.
	loc := time.UTC
	tz := strings.TrimSpace(r.URL.Query().Get("tz"))
	if tz == "" {
		tz = localTimezoneName()
	}
	if tz != "" {
		if l, lerr := time.LoadLocation(tz); lerr == nil {
			loc = l
		} else {
			// Named rather than silently falling back: a typo'd zone that
			// quietly becomes UTC is how a schedule ends up firing in the
			// middle of the night with nothing to explain it.
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":    false,
				"error": fmt.Sprintf("unknown timezone %q", tz),
			})
			return
		}
	}

	out := map[string]any{"ok": true, "human": scheduler.Describe(expr), "timezone": loc.String()}
	if next := sched.Next(time.Now().In(loc)); !next.IsZero() {
		out["next_run_at"] = next.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, out)
}

package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/runner"
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

// countLiveServices resolves every bot a swarm references (the same
// planner.Resolve every real plan/save already goes through) and counts
// how many of their declared services are switched off connection: demo.
func countLiveServices(sw *schema.Nanoswarm, botsDir string) (live, total int) {
	resolved, err := planner.Resolve(sw, botsDir)
	if err != nil {
		return 0, 0
	}
	for _, b := range resolved.Bots {
		for _, svc := range b.Nanobot.Spec.Services {
			total++
			if svc.Connection != "" && svc.Connection != schema.ConnectionDemo {
				live++
			}
		}
	}
	return live, total
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
		live, total := countLiveServices(sw, s.BotsDir)
		summary := SwarmSummary{
			Path: relPath, Name: sw.Metadata.Name, Description: sw.Metadata.Description,
			ServicesLive: live, ServicesTotal: total,
		}
		if last := lastRunFor(allRuns, sw.Metadata.Name); last != nil {
			summary.LastRunID = last.ID
			summary.LastRunStatus = string(last.GetStatus())
			summary.LastRunAt = last.StartedAt.Format(time.RFC3339)
			summary.LastRunTrigger = last.TriggeredBy
		}
		swarms = append(swarms, summary)
	}
	writeJSON(w, http.StatusOK, nonNil(swarms))
}

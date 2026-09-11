package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/planner"
	"github.com/redbotster/nanobots/internal/schema"
)

// SwarmSummary is what the WebUI's swarm list needs to render a gallery
// entry and then load the full canvas for one — see web/src/pages/Swarms.tsx.
// ServicesLive/ServicesTotal let the gallery show "how real is this swarm
// right now" at a glance, without opening it — a swarm resolution failure
// (a broken bot ref) just leaves both at 0 rather than failing the whole
// listing.
type SwarmSummary struct {
	Path          string `json:"path"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ServicesLive  int    `json:"services_live"`
	ServicesTotal int    `json:"services_total"`
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
		swarms = append(swarms, SwarmSummary{
			Path: relPath, Name: sw.Metadata.Name, Description: sw.Metadata.Description,
			ServicesLive: live, ServicesTotal: total,
		})
	}
	writeJSON(w, http.StatusOK, nonNil(swarms))
}

package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// SwarmSummary is what the WebUI's swarm list needs to render a gallery
// entry and then load the full canvas for one — see web/src/pages/Swarms.tsx.
type SwarmSummary struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
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
		swarms = append(swarms, SwarmSummary{
			Path: relPath, Name: sw.Metadata.Name, Description: sw.Metadata.Description,
		})
	}
	writeJSON(w, http.StatusOK, nonNil(swarms))
}

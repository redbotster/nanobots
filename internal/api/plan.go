package api

import (
	"net/http"
	"os"

	"github.com/redbotster/nanobots/internal/planner"
)

type snapCheckJSON struct {
	From, To         string
	FromType, ToType string `json:",omitempty"`
	OK               bool
	Error            string `json:"error,omitempty"`
}

// botInstanceJSON tells the WebUI which bots/<dir> a swarm's instance id
// (e.g. "recap") actually resolves to (e.g. "recap-emails-to-pdf") — the two
// are different namespaces (swarm-local instance id vs. bot directory id),
// and the canvas needs both: the instance id to key run log entries, the
// directory id to look the bot's full definition up in GET /api/bots.
type botInstanceJSON struct {
	InstanceID string `json:"instance_id"`
	BotID      string `json:"bot_id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
}

type planResponse struct {
	Swarm string            `json:"swarm"`
	Order []string          `json:"order,omitempty"`
	Bots  []botInstanceJSON `json:"bots"`
	Error string            `json:"error,omitempty"`
	Snaps []snapCheckJSON   `json:"snaps"`
	OK    bool              `json:"ok"`
}

// handlePlan exposes `nanobots plan` over HTTP — the WebUI calls this before
// offering to run a swarm, and to render the canvas's type-checked snaps.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	path, err := swarmPathFromRequest(s.swarmsDir(), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := planner.Plan(path, s.BotsDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, buildPlanResponse(result.Resolved.Swarm.Metadata.Name, result, nil))
}

// handleSwarmYAML serves a swarm file's raw source for the WebUI's YAML
// drawer.
//
// It goes through swarmPathFromRequest like every other path-taking handler
// now. It used to guard with `strings.Contains(path, "..")` and then
// os.ReadFile the value as given — but an absolute path contains no "..", so
// ?path=/Users/you/.ssh/id_rsa returned the file. nanobotd binds loopback,
// which is no defence here: it answers with Access-Control-Allow-Origin: *
// and no auth, so any page in the user's browser could read the response.
func (s *Server) handleSwarmYAML(w http.ResponseWriter, r *http.Request) {
	path, err := swarmPathFromRequest(s.swarmsDir(), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(raw)
}

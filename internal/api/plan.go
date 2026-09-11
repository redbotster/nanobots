package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

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
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("?path=<swarm.yaml> is required"))
		return
	}
	result, err := planner.Plan(path, s.BotsDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp := planResponse{Swarm: result.Resolved.Swarm.Metadata.Name, OK: result.OK()}
	for instanceID, rb := range result.Resolved.Bots {
		botID, _, _ := strings.Cut(rb.Ref.Use, "@")
		if botID == "" {
			botID = rb.Ref.Path
		}
		resp.Bots = append(resp.Bots, botInstanceJSON{
			InstanceID: instanceID, BotID: botID,
			Name: rb.Nanobot.Metadata.Name, Version: rb.Nanobot.Metadata.Version,
		})
	}
	if result.DAGErr != nil {
		resp.Error = result.DAGErr.Error()
	} else if order, err := result.DAG.TopoSort(); err == nil {
		resp.Order = order
	}
	for _, c := range result.Snaps {
		sc := snapCheckJSON{From: c.Snap.From, To: c.Snap.To, OK: c.OK}
		if c.OK {
			sc.FromType, sc.ToType = c.FromType.String(), c.ToType.String()
		} else {
			sc.Error = c.Err.Error()
		}
		resp.Snaps = append(resp.Snaps, sc)
	}
	resp.Bots = nonNil(resp.Bots)
	resp.Snaps = nonNil(resp.Snaps)
	writeJSON(w, http.StatusOK, resp)
}

// handleSwarmYAML serves a swarm file's raw source for the WebUI's YAML
// drawer. nanobotd only ever binds loopback (see internal/daemon), but this
// still refuses to walk outside the repo on a malformed path.
func (s *Server) handleSwarmYAML(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || strings.Contains(path, "..") {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid ?path="))
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

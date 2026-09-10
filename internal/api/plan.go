package api

import (
	"fmt"
	"net/http"

	"github.com/redbotster/nanobots/internal/planner"
)

type snapCheckJSON struct {
	From, To         string
	FromType, ToType string `json:",omitempty"`
	OK               bool
	Error            string `json:"error,omitempty"`
}

type planResponse struct {
	Swarm string          `json:"swarm"`
	Order []string        `json:"order,omitempty"`
	Error string          `json:"error,omitempty"`
	Snaps []snapCheckJSON `json:"snaps"`
	OK    bool            `json:"ok"`
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
	writeJSON(w, http.StatusOK, resp)
}

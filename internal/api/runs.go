package api

import (
	"fmt"
	"net/http"

	"github.com/redbotster/nanobots/internal/runner"
)

type startRunRequest struct {
	SwarmPath string `json:"swarm_path"`
}

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	var req startRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.SwarmPath == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("swarm_path is required"))
		return
	}
	run, err := s.Orchestrator.ExecuteSwarm(req.SwarmPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.Runs.Add(run)
	writeJSON(w, http.StatusAccepted, runToJSON(run))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs := s.Runs.List()
	out := make([]any, len(runs))
	for i, run := range runs {
		out[i] = runToJSON(run)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Runs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, runToJSON(run))
}

type decideApprovalRequest struct {
	Approved  bool   `json:"approved"`
	DecidedBy string `json:"decided_by"`
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	run, err := s.Runs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	var req decideApprovalRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.DecidedBy == "" {
		req.DecidedBy = "you"
	}
	if err := run.Decide(r.PathValue("approvalID"), req.Approved, req.DecidedBy); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func runToJSON(run *runner.Run) map[string]any {
	approvals := run.PendingApprovals()
	pending := make([]*runner.PendingApproval, len(approvals))
	copy(pending, approvals)
	return map[string]any{
		"id":                run.ID,
		"swarm_name":        run.SwarmName,
		"status":            run.Status,
		"started_at":        run.StartedAt,
		"finished_at":       run.FinishedAt,
		"error":             run.Error,
		"log":               run.LogEntries(),
		"pending_approvals": pending,
		"outputs":           run.AllOutputs(),
	}
}

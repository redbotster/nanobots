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
		out[i] = runSummaryToJSON(run)
	}
	writeJSON(w, http.StatusOK, out)
}

// runSummaryToJSON is what the list endpoint returns: everything a run list
// renders, and nothing it doesn't.
//
// The list used to return whole runs, logs and outputs included. With only
// nine runs on this machine that was already 22KB, 76% of it log and output
// text no list view has ever displayed — and both the Runs page and the
// approval poller fetch it every two seconds, so ~1.3 MB/min to render nine
// rows. Run history is now capped at 200 and persisted, so that was on its
// way to tens of MB a minute. GET /api/runs/{id} still returns the whole
// thing; that's the endpoint that has a reader for it.
func runSummaryToJSON(run *runner.Run) map[string]any {
	return map[string]any{
		"id":           run.ID,
		"swarm_name":   run.SwarmName,
		"status":       run.GetStatus(),
		"started_at":   run.StartedAt,
		"finished_at":  run.GetFinishedAt(),
		"error":        run.GetError(),
		"triggered_by": run.TriggeredBy,
		"swarm_path":   run.SwarmPath,
		// A count, not the approvals themselves — enough for a badge and
		// for "N runs are waiting on you", which is all a list needs.
		"pending_approval_count": len(run.PendingApprovals()),
	}
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
		"status":            run.GetStatus(),
		"started_at":        run.StartedAt,
		"finished_at":       run.GetFinishedAt(),
		"error":             run.GetError(),
		"triggered_by":      run.TriggeredBy,
		"swarm_path":        run.SwarmPath,
		"log":               run.LogEntries(),
		"pending_approvals": pending,
		"outputs":           run.AllOutputs(),
	}
}

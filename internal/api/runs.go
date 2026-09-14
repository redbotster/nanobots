package api

import (
	"fmt"
	"net/http"
	"sort"

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
	// Resolved against the swarms directory rather than trusted as given:
	// this endpoint executes what it's pointed at, so an unconstrained path
	// is worse than a read.
	swarmPath, err := swarmPathFromRequest(s.swarmsDir(), req.SwarmPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.Orchestrator.ExecuteSwarm(swarmPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.Runs.Add(run)
	writeJSON(w, http.StatusAccepted, runToJSON(run))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs := s.Runs.List()
	// Newest first, and sorted at all: RunStore.List ranges over a map, so
	// the order was whatever Go felt like that iteration. Every client
	// re-sorted anyway, but an unstable order also means an unstable
	// response body — which would defeat the conditional GET below by
	// producing a different ETag every poll for identical data.
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].ID < runs[j].ID // a tiebreak, so ties are stable too
		}
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	out := make([]any, len(runs))
	for i, run := range runs {
		out[i] = runSummaryToJSON(run)
	}
	writeJSONCached(w, r, http.StatusOK, out)
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
	out := map[string]any{
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
	// Only when true. This list is polled every two seconds and the field
	// is false for almost every run ever; a key per row to say "no" is the
	// kind of thing that turns a lean summary back into a fat one.
	if run.WasStoppedByUser() {
		out["stopped_by_user"] = true
	}
	return out
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
		"id":           run.ID,
		"swarm_name":   run.SwarmName,
		"status":       run.GetStatus(),
		"started_at":   run.StartedAt,
		"finished_at":  run.GetFinishedAt(),
		"error":        run.GetError(),
		"triggered_by": run.TriggeredBy,
		// Whether someone stopped this on purpose. Without it a run you
		// ended yourself is indistinguishable from one that broke — same
		// red dot, same "failed".
		"stopped_by_user":   run.WasStoppedByUser(),
		"swarm_path":        run.SwarmPath,
		"log":               run.LogEntries(),
		"pending_approvals": pending,
		"outputs":           run.AllOutputs(),
		// Bots that failed while the swarm was told to continue without
		// them. A run carrying one of these is a success with a hole in
		// it, and the UI renders it as a warning rather than plain green.
		"tolerated": run.GetTolerated(),
		// Services this run reached through fixtures rather than a real
		// account. A run made of demo data succeeds and looks exactly like
		// a real one, and that is the default.
		"demo_services": run.DemoServices(),
	}
}

// handleCancelRun stops a run that is still going.
//
// Until this existed there was no way to stop anything. A bot that hangs
// holds its container for the whole max_runtime ceiling — up to thirty
// minutes — and the only available action was to watch. On one real machine
// that ceiling was hit 41 times by a single swarm.
//
// Answers 409 rather than 200 for a run that has already finished: "stopped"
// and "it was already over" are different outcomes and the caller may care.
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Runs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if !run.Stop() {
		writeError(w, http.StatusConflict, fmt.Errorf(
			"run %s already finished (%s)", run.ID, run.GetStatus()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": run.ID})
}

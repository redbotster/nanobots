// The foundry's REST+SSE surface — the escalation path behind /api/compose
// (compose.go): when the composer declares a gap, the WebUI's "want me to
// build one?" button calls POST /api/foundry to kick off a sandboxed
// coding agent, then polls/subscribes exactly the way it already does for
// a swarm run (see runs.go, sse.go) since internal/foundry.Job embeds
// *runner.Run for precisely that reason. foundryJobToJSON reads Job's own
// fields (BotID, Iterations, ConformOK, Outcome, BotPreview) through their
// mutex-guarded accessors, and the embedded Run's fields (ID, Status,
// Error, StartedAt, FinishedAt) the same direct way runToJSON already does
// in runs.go — consistent with that existing, accepted pattern rather than
// inventing a different one for this new endpoint.
package api

import (
	"fmt"
	"net/http"

	"github.com/redbotster/nanobots/internal/foundry"
	"github.com/redbotster/nanobots/internal/schema"
)

type startFoundryJobRequest struct {
	Request           string              `json:"request"`
	MissingCapability string              `json:"missing_capability"`
	SuggestedInputs   []schema.InputPort  `json:"suggested_inputs,omitempty"`
	SuggestedOutputs  []schema.OutputPort `json:"suggested_outputs,omitempty"`
}

func (s *Server) handleStartFoundryJob(w http.ResponseWriter, r *http.Request) {
	if s.Foundry == nil || s.FoundryJobs == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the foundry isn't configured on this server"))
		return
	}
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("1Claw isn't configured yet — add ONECLAW_API_KEY first"))
		return
	}
	var req startFoundryJobRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.MissingCapability == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing_capability is required"))
		return
	}

	job, err := s.Foundry.StartJob(req.Request, req.MissingCapability, req.SuggestedInputs, req.SuggestedOutputs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.FoundryJobs.Add(job)
	writeJSON(w, http.StatusAccepted, foundryJobToJSON(job))
}

func (s *Server) handleListFoundryJobs(w http.ResponseWriter, r *http.Request) {
	if s.FoundryJobs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	jobs := s.FoundryJobs.List()
	out := make([]any, len(jobs))
	for i, j := range jobs {
		out[i] = foundryJobToJSON(j)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetFoundryJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.getFoundryJob(w, r)
	if err != nil {
		return
	}
	writeJSON(w, http.StatusOK, foundryJobToJSON(job))
}

func (s *Server) handleDecideFoundryReview(w http.ResponseWriter, r *http.Request) {
	job, err := s.getFoundryJob(w, r)
	if err != nil {
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
	if err := job.Decide(r.PathValue("approvalID"), req.Approved, req.DecidedBy); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleFoundryJobEvents mirrors sse.go's handleRunEvents exactly, over a
// Job's embedded Run — same replay-then-subscribe shape.
func (s *Server) handleFoundryJobEvents(w http.ResponseWriter, r *http.Request) {
	job, err := s.getFoundryJob(w, r)
	if err != nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for _, entry := range job.LogEntries() {
		writeSSEEvent(w, entry)
	}
	flusher.Flush()

	ch := job.Subscribe()
	defer job.Unsubscribe(ch)
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, entry)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) getFoundryJob(w http.ResponseWriter, r *http.Request) (*foundry.Job, error) {
	if s.FoundryJobs == nil {
		err := fmt.Errorf("the foundry isn't configured on this server")
		writeError(w, http.StatusNotFound, err)
		return nil, err
	}
	job, err := s.FoundryJobs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return nil, err
	}
	return job, nil
}

func foundryJobToJSON(job *foundry.Job) map[string]any {
	out := map[string]any{
		"id":                 job.ID,
		"status":             job.GetStatus(),
		"started_at":         job.StartedAt,
		"finished_at":        job.GetFinishedAt(),
		"error":              job.GetError(),
		"log":                job.LogEntries(),
		"pending_approvals":  job.PendingApprovals(),
		"request":            job.Request,
		"missing_capability": job.MissingCapability,
		"bot_id":             job.BotID(),
		"iterations":         job.Iterations(),
		"conform_ok":         job.ConformOK(),
		"outcome":            job.Outcome(),
	}
	if preview := job.BotPreview(); preview != nil {
		out["bot"] = BotSummary{
			ID: preview.ID, Name: preview.Name, Version: preview.Version,
			Description: preview.Description, Harness: preview.Harness,
			Tags: nonNil(preview.Tags), Services: nonNil(preview.Services),
			Inputs: nonNil(preview.Inputs), Outputs: nonNil(preview.Outputs),
			Guardrails: preview.Guardrails,
		}
	}
	return out
}

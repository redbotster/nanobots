package api

import (
	"net/http"
	"strings"

	"errors"
	"github.com/redbotster/nanobots/internal/memory"
	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

// depsFromRequest authenticates a container's callback by its per-run token
// (see internal/step.RemoteDeps and internal/runner.CallbackRegistry) — the
// token means nothing beyond "this one container, this one bot instance,
// this one run"; it is never a real 1Claw credential.
func (s *Server) depsFromRequest(r *http.Request) (step.Deps, bool) {
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || token == "" {
		return nil, false
	}
	d, err := s.Callbacks.Lookup(token)
	if err != nil {
		return nil, false
	}
	return d, true
}

func writeCallbackError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
}

func writeCallbackResult(w http.ResponseWriter, result any) {
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) handleStepServiceCall(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct {
		Service schema.Service `json:"service"`
		Op      string         `json:"op"`
		Params  map[string]any `json:"params"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	result, err := deps.ServiceCall(req.Service, req.Op, req.Params)
	if err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, result)
}

func (s *Server) handleStepAIGenerate(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct {
		Prompt string       `json:"prompt"`
		Model  schema.Model `json:"model"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	result, err := deps.AIGenerate(req.Prompt, req.Model)
	if err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, result)
}

func (s *Server) handleStepMemoryGet(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct{ Namespace, Key string }
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	value, found, err := deps.MemoryGet(req.Namespace, req.Key)
	if err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, map[string]any{"value": value, "found": found})
}

func (s *Server) handleStepMemoryPut(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct{ Namespace, Key, Value string }
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	if err := deps.MemoryPut(req.Namespace, req.Key, req.Value); err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, nil)
}

func (s *Server) handleStepApprove(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	// Tagged, because the wire name is risk_tier: encoding/json matches
	// field names case-insensitively but not across an underscore, so an
	// untagged RiskTier silently dropped every containerised approval's
	// risk tier — the one piece of context an `approve` step exists to put
	// in front of a person.
	var req struct {
		Summary  string `json:"summary"`
		RiskTier string `json:"risk_tier"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	approved, decidedBy, err := deps.Approve(req.Summary, req.RiskTier)
	if err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, map[string]any{"approved": approved, "decided_by": decidedBy})
}

func (s *Server) handleStepWebFetch(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct {
		Params map[string]any `json:"params"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	result, err := deps.WebFetch(req.Params)
	if err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, result)
}

func (s *Server) handleStepNotify(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct{ Message, Channel string }
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	if err := deps.Notify(req.Message, req.Channel); err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, nil)
}

// handleStepMemoryRecall answers a bot's natural-language question from
// whatever memory backend this daemon is configured with. Unlike
// memory.get, a backend that can't do this returns an error rather than an
// empty answer — see internal/memory's ErrNoRecall.
func (s *Server) handleStepMemoryRecall(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct{ Namespace, Question string }
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	answer, err := deps.MemoryRecall(req.Namespace, req.Question)
	if errors.Is(err, memory.ErrNoRecall) {
		// Not an error on the wire: "this deployment has no recall" is a
		// fact the container needs to distinguish from "recall broke", and
		// an error string can't be type-asserted after crossing HTTP.
		writeCallbackResult(w, map[string]any{"answer": "", "supported": false})
		return
	}
	if err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, map[string]any{"answer": answer, "supported": true})
}

// handleStepMemoryRemember records an observation for a recall-capable
// backend to derive from later.
func (s *Server) handleStepMemoryRemember(w http.ResponseWriter, r *http.Request) {
	deps, ok := s.depsFromRequest(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req struct{ Namespace, Text string }
	if err := decodeJSON(r, &req); err != nil {
		writeCallbackError(w, err)
		return
	}
	if err := deps.MemoryRemember(req.Namespace, req.Text); err != nil {
		writeCallbackError(w, err)
		return
	}
	writeCallbackResult(w, map[string]any{"ok": true})
}

// Lab's Team-engine configuration: which coding-agent CLI a delegation
// actually runs, and the credential each one needs — both previously
// invisible and unchangeable without editing ~/.secrets/nanobots.env and
// restarting nanobotd. Found live: a real request silently ran on a
// Gemini free-tier key with a five-to-twenty-request quota and burned
// through it before failing, with nothing in the app saying it would, or
// offering a way to pick differently.
package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/redbotster/nanobots/internal/team"
)

type labEngineRole struct {
	Role       string      `json:"role"`
	Engine     team.Engine `json:"engine"`     // effective: the override, or the default
	Overridden bool        `json:"overridden"` // false means "engine" is just the default
}

type labEnginesResponse struct {
	DefaultEngine    team.Engine     `json:"default_engine"` // "" means nothing is configured at all
	ClaudeConfigured bool            `json:"claude_configured"`
	GeminiConfigured bool            `json:"gemini_configured"`
	Roles            []labEngineRole `json:"roles"`
}

// handleLabEnginesStatus reports which engines have a credential, which
// one is the default, and every role that has ever been delegated to
// (internal/team.Roles scans its workspace directory — there is no fixed
// catalog) with its effective engine.
func (s *Server) handleLabEnginesStatus(w http.ResponseWriter, r *http.Request) {
	if s.LabEngines == nil {
		writeJSON(w, http.StatusOK, labEnginesResponse{Roles: []labEngineRole{}})
		return
	}
	roleNames, err := team.Roles(s.LabTeamDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sort.Strings(roleNames)
	overrides := s.LabEngines.RoleOverrides()
	roleStatuses := make([]labEngineRole, 0, len(roleNames))
	for _, role := range roleNames {
		override, has := overrides[role]
		roleStatuses = append(roleStatuses, labEngineRole{
			Role:       role,
			Engine:     s.LabEngines.EngineFor(role),
			Overridden: has && override != "",
		})
	}
	writeJSON(w, http.StatusOK, labEnginesResponse{
		DefaultEngine:    s.LabEngines.Default(),
		ClaudeConfigured: s.LabClaudeConfigured,
		GeminiConfigured: s.LabGeminiConfigured,
		Roles:            roleStatuses,
	})
}

type setEngineRequest struct {
	Engine team.Engine `json:"engine"`
}

// engineConfigured reports whether the given engine actually has a
// credential — refusing to select an engine with nothing behind it here
// is a clearer failure than letting it through to the next real
// delegation, which is a container that starts, fails, and reports a raw
// "ANTHROPIC_API_KEY isn't set" from inside internal/team.Run instead.
func (s *Server) engineConfigured(e team.Engine) bool {
	switch e {
	case team.EngineClaude:
		return s.LabClaudeConfigured
	case team.EngineGemini:
		return s.LabGeminiConfigured
	default:
		return false
	}
}

// handleSetDefaultLabEngine changes the engine used by any role with no
// override of its own. Takes effect on Lab's very next delegation — no
// restart, since s.LabEngines is the same pointer lab.Config.Engines holds.
func (s *Server) handleSetDefaultLabEngine(w http.ResponseWriter, r *http.Request) {
	if s.LabEngines == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("Lab isn't configured on this server"))
		return
	}
	var req setEngineRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.engineConfigured(req.Engine) {
		writeError(w, http.StatusBadRequest, fmt.Errorf(
			"%s isn't configured — paste its API key in Settings first, then restart nanobotd once", req.Engine))
		return
	}
	if err := s.LabEngines.SetDefault(req.Engine); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]team.Engine{"default_engine": req.Engine})
}

// handleSetRoleLabEngine overrides one role's engine; an empty engine
// clears the override, falling back to the default again.
func (s *Server) handleSetRoleLabEngine(w http.ResponseWriter, r *http.Request) {
	if s.LabEngines == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("Lab isn't configured on this server"))
		return
	}
	role := r.PathValue("role")
	var req setEngineRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Engine != "" && !s.engineConfigured(req.Engine) {
		writeError(w, http.StatusBadRequest, fmt.Errorf(
			"%s isn't configured — paste its API key in Settings first, then restart nanobotd once", req.Engine))
		return
	}
	if err := s.LabEngines.SetRoleEngine(role, req.Engine); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, labEngineRole{
		Role:       role,
		Engine:     s.LabEngines.EngineFor(role),
		Overridden: req.Engine != "",
	})
}

type setEngineKeyRequest struct {
	Token string `json:"token"`
}

// engineKeyName maps an engine to its credential's path in the secrets
// store — the same flat "provider/key_name" convention every other
// pasted token here already uses.
func engineKeyName(engine string) (string, error) {
	switch team.Engine(engine) {
	case team.EngineClaude:
		return "anthropic/api_key", nil
	case team.EngineGemini:
		return "gemini/api_key", nil
	default:
		return "", fmt.Errorf("unknown engine %q — must be %q or %q", engine, team.EngineClaude, team.EngineGemini)
	}
}

// handleSetLabEngineKey stores a pasted Anthropic or Gemini API key —
// deliberately separate from /api/connections/{service}: these aren't a
// service a bot declares, they're the credential a Team engine's own CLI
// binary speaks its vendor's API with directly (see docs/team.md).
//
// Unlike the engine choice itself, this needs a restart to take effect:
// internal/team.Config.AnthropicAPIKey/GeminiAPIKey are resolved once at
// daemon startup, the same as every other credential this build reads at
// boot. Said plainly in the response rather than implied.
func (s *Server) handleSetLabEngineKey(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("no secrets backend configured — see docs/secrets.md"))
		return
	}
	key, err := engineKeyName(r.PathValue("engine"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req setEngineKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("token is required"))
		return
	}
	if err := s.Secrets.Put(key, strings.TrimSpace(req.Token)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"saved":  key,
		"notice": "restart nanobotd to make this key available to Team",
	})
}

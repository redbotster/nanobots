package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/roles"
)

// The role library: the perspectives a review team is built from.
//
// Exposed as its own resource rather than folded into /api/fleet because a
// role is not a bot. The Fleet's members are bots whose instructions you
// changed; roles are a shared vocabulary the review bots draw on, and a
// user with no tuned bots at all still has a library worth editing.
//
// See internal/roles for why edits live outside the repo, and
// docs/supervisors.md for why a role is an input rather than a bot.

type rolesResponse struct {
	Roles []roles.Role `json:"roles"`
	// Roster is exactly what a review board is shown. Returned so the UI
	// can display the real thing rather than a reconstruction of it —
	// "what will the model actually see" is the question someone editing a
	// role is trying to answer.
	Roster string `json:"roster"`
	// Error carries a problem reading the library without failing the
	// request: a corrupt overrides file should show as a warning next to a
	// still-usable shipped catalog, not as an empty page.
	Error string `json:"error,omitempty"`
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if s.Roles == nil {
		writeJSON(w, http.StatusOK, rolesResponse{Roles: []roles.Role{}})
		return
	}
	list, err := s.Roles.List()
	roster, rosterErr := s.Roles.Roster()
	resp := rolesResponse{Roles: nonNil(list), Roster: roster}
	if err != nil {
		resp.Error = err.Error()
	} else if rosterErr != nil {
		resp.Error = rosterErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

type setRoleRequest struct {
	Name  string `json:"name"`
	Focus string `json:"focus"`
	// Retired switches a role off without losing its wording. Sent alone
	// (with no name/focus) to toggle; sent with them, it is applied too.
	Retired *bool `json:"retired,omitempty"`
}

// handleSetRole edits a role, adds one, or switches one off.
//
// POST rather than PUT/PATCH for the same reason as everything else here:
// this API is consumed by one WebUI, and a second verb would be ceremony
// rather than clarity.
func (s *Server) handleSetRole(w http.ResponseWriter, r *http.Request) {
	if s.Roles == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("no role library is configured on this daemon"))
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var req setRoleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if req.Retired != nil && strings.TrimSpace(req.Name) == "" && strings.TrimSpace(req.Focus) == "" {
		if err := s.Roles.SetRetired(id, *req.Retired); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.handleListRoles(w, r)
		return
	}

	if err := s.Roles.Set(id, req.Name, req.Focus); err != nil {
		// A validation failure is the user's to fix, not a server fault —
		// and the message says exactly what is wrong with what they typed.
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Retired != nil {
		if err := s.Roles.SetRetired(id, *req.Retired); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.handleListRoles(w, r)
}

// handleResetRole puts a shipped role back to what it shipped with, or
// removes one the user added.
func (s *Server) handleResetRole(w http.ResponseWriter, r *http.Request) {
	if s.Roles == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("no role library is configured on this daemon"))
		return
	}
	if err := s.Roles.Reset(strings.TrimSpace(r.PathValue("id"))); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.handleListRoles(w, r)
}

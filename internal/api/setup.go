// The one credential every other feature in this build depends on
// (ONECLAW_API_KEY) previously had no in-app setup path at all — every
// other service (Slack, GitHub, Google, X, LinkedIn, Stripe, HubSpot) got
// a one-click Connect story this build, while the most foundational
// credential still told a human to go open a text editor. This closes
// that gap the same way: paste it, save it, no editor required.
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

type setupOneClawKeyRequest struct {
	APIKey string `json:"api_key"`
}

// handleSetupOneClawKey validates the key actually authenticates (a real,
// side-effect-free ListAgents call against a throwaway client, so a typo
// is caught immediately rather than after a restart) and, if so, writes it
// to the dotenv file every other credential in this build already reads
// from. It still requires restarting nanobotd to take effect — the same
// contract this build already has for GOOGLE_OAUTH_CLIENT_ID and friends —
// rather than threading a hot-reloadable client through every handler,
// orchestrator, and foundry config that holds one today.
func (s *Server) handleSetupOneClawKey(w http.ResponseWriter, r *http.Request) {
	var req setupOneClawKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("api_key is required"))
		return
	}

	check := oneclaw.NewClient(key)
	if _, err := check.ListAgents(); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("that key didn't authenticate against 1Claw: %w", err))
		return
	}

	if err := oneclaw.WriteEnvValue(s.EnvFilePath, "ONECLAW_API_KEY", key); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restart_required": true})
}

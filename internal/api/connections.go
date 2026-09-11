// The WebUI's unified "Connect" experience: one place (Settings) to link
// Slack, GitHub, or Google, instead of the CLI-only `nanobots connect`
// this used to require — matching this build's "make it dead simple, never
// make the user understand an API key" design principle from day one.
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/redbotster/nanobots/internal/google"
	"github.com/redbotster/nanobots/internal/linkedin"
	"github.com/redbotster/nanobots/internal/oauth2pkce"
	"github.com/redbotster/nanobots/internal/x"
)

// vaultKeyFor maps a service name to where its credential lives in the
// shared "nanobots-main" vault — the same paths internal/step's
// {google,github,slack,x,linkedin}_live.go read from by default. X and
// LinkedIn are OAuth flows, like Google, so this key is only ever written
// by their own handleConnect*Start handlers below, never by
// handleConnectToken's paste-a-token path.
var vaultKeyFor = map[string]string{
	"google":   "google/refresh_token",
	"slack":    "slack/bot_token",
	"github":   "github/token",
	"stripe":   "stripe/secret_key",
	"hubspot":  "hubspot/token",
	"x":        "x/refresh_token",
	"linkedin": "linkedin/refresh_token",
}

type connectionStatus struct {
	Service   string `json:"service"`
	Connected bool   `json:"connected"`
}

// handleConnectionsStatus reports which of google/slack/github have a
// credential in the vault already. Each check is a real (small, paid)
// 1Claw vault read — acceptable for a page a human visits occasionally, not
// something to poll.
func (s *Server) handleConnectionsStatus(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeJSON(w, http.StatusOK, []connectionStatus{})
		return
	}
	vault, err := s.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	statuses := make([]connectionStatus, 0, len(vaultKeyFor))
	for _, service := range []string{"google", "slack", "github", "stripe", "hubspot", "x", "linkedin"} {
		_, err := s.OneClaw.GetSecret(vault.ID, vaultKeyFor[service])
		connected := err == nil
		if service == "linkedin" && !connected {
			// LinkedIn may have connected without a refresh token at all
			// (see internal/step/linkedin_live.go) — a stored access token
			// alone still counts as connected.
			_, err := s.OneClaw.GetSecret(vault.ID, "linkedin/access_token")
			connected = err == nil
		}
		statuses = append(statuses, connectionStatus{Service: service, Connected: connected})
	}
	writeJSON(w, http.StatusOK, statuses)
}

type connectTokenRequest struct {
	Token string `json:"token"`
}

// handleConnectToken stores a pasted static token (a Slack bot token or a
// GitHub personal access token) as a vault secret — the `api_key_vault`
// connection method from docs/connections.md, now reachable from the WebUI
// instead of only ever being a CLI/manual-paste story.
func (s *Server) handleConnectToken(w http.ResponseWriter, r *http.Request) {
	service := r.PathValue("service")
	key, ok := vaultKeyFor[service]
	if !ok || service == "google" || service == "x" || service == "linkedin" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown or unsupported service %q for a pasted token", service))
		return
	}
	var req connectTokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("token is required"))
		return
	}
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("1Claw isn't configured yet — add ONECLAW_API_KEY first"))
		return
	}
	vault, err := s.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.OneClaw.PutSecret(vault.ID, key, strings.TrimSpace(req.Token)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionStatus{Service: service, Connected: true})
}

// handleConnectGoogleStart runs the real interactive OAuth round trip
// server-side (nanobotd and the browser it opens are on the same machine in
// this local-first build) and stores the resulting refresh token — the
// WebUI equivalent of `nanobots connect google`. The request blocks for as
// long as the human takes to approve in their browser (up to 5 minutes,
// google.Connect's own timeout); the WebUI shows a spinner for that.
func (s *Server) handleConnectGoogleStart(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("1Claw isn't configured yet — add ONECLAW_API_KEY first"))
		return
	}
	clientID, err := google.LoadClientID("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if clientID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf(
			"GOOGLE_OAUTH_CLIENT_ID isn't set — add it to ~/.secrets/nanobots.env (see docs/connections.md), then restart nanobotd"))
		return
	}
	vault, err := s.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	tr, err := google.Connect(r.Context(), clientID, google.DefaultScopes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if tr.RefreshToken == "" {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("google didn't return a refresh token — try again"))
		return
	}
	if err := s.OneClaw.PutSecret(vault.ID, vaultKeyFor["google"], tr.RefreshToken); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionStatus{Service: "google", Connected: true})
}

// handleConnectXStart mirrors handleConnectGoogleStart exactly, for an X
// (Twitter) "Native App" (public) OAuth client — see internal/x's package
// doc for the app-registration assumptions.
func (s *Server) handleConnectXStart(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("1Claw isn't configured yet — add ONECLAW_API_KEY first"))
		return
	}
	clientID, err := x.LoadClientID("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if clientID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf(
			"X_OAUTH_CLIENT_ID isn't set — add it to ~/.secrets/nanobots.env (see docs/connections.md), then restart nanobotd"))
		return
	}
	vault, err := s.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	tr, err := oauth2pkce.Connect(r.Context(), x.OAuthConfig(clientID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if tr.RefreshToken == "" {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("x didn't return a refresh token — try again"))
		return
	}
	if err := s.OneClaw.PutSecret(vault.ID, vaultKeyFor["x"], tr.RefreshToken); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionStatus{Service: "x", Connected: true})
}

// handleConnectLinkedInStart mirrors handleConnectGoogleStart, but LinkedIn
// needs a client secret too and may not hand back a refresh token at all
// (see internal/step/linkedin_live.go's linkedinTokenSource) — in that case
// the access token itself is stored instead, used as-is until it expires.
func (s *Server) handleConnectLinkedInStart(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("1Claw isn't configured yet — add ONECLAW_API_KEY first"))
		return
	}
	clientID, err := linkedin.LoadClientID("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	clientSecret, err := linkedin.LoadClientSecret("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if clientID == "" || clientSecret == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf(
			"LINKEDIN_OAUTH_CLIENT_ID / LINKEDIN_OAUTH_CLIENT_SECRET aren't both set — add them to ~/.secrets/nanobots.env (see docs/connections.md), then restart nanobotd"))
		return
	}
	vault, err := s.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	tr, err := oauth2pkce.Connect(r.Context(), linkedin.OAuthConfig(clientID, clientSecret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if tr.RefreshToken != "" {
		if err := s.OneClaw.PutSecret(vault.ID, vaultKeyFor["linkedin"], tr.RefreshToken); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		if err := s.OneClaw.PutSecret(vault.ID, "linkedin/access_token", tr.AccessToken); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, connectionStatus{Service: "linkedin", Connected: true})
}

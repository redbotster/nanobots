// The WebUI's unified "Connect" experience: one place (Settings) to link
// Slack, GitHub, or Google, instead of the CLI-only `nanobots connect`
// this used to require — matching this build's "make it dead simple, never
// make the user understand an API key" design principle from day one.
package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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

// connectionServices is the order the status list comes back in. Fixed and
// explicit because the response is ETag-cached: a map range here would
// reshuffle the list on every call and make the ETag change when nothing
// had.
var connectionServices = []string{"google", "slack", "github", "stripe", "hubspot", "x", "linkedin"}

// connectionTTL is how long a cached status list is served before being
// re-read from the vault.
//
// The cache is invalidated exactly, by every handler in this file that
// writes a credential, so the TTL is not how correctness is maintained —
// it is the backstop for the one case this app cannot observe: a secret
// added or removed somewhere else, in 1Claw's own dashboard or by another
// install sharing the account. Without it the app would go on claiming
// "not connected" about something that is, which is the failure mode this
// codebase cares most about. A minute is long enough that moving between
// Settings, the bot library and the builder costs nothing, and short enough
// that an out-of-band change fixes itself while the user is still looking
// at the page.
const connectionTTL = time.Minute

// handleConnectionsStatus reports which services have a credential in the
// vault already.
//
// Each check is a real 1Claw vault read at roughly a second of round trip,
// and there are eight of them (seven services plus LinkedIn's fallback).
// Done in sequence that was 7.8s, measured against the live daemon, every
// single call — and this is not the occasional-settings-page request an
// earlier comment here claimed it was. Four places load it: SettingsPage,
// BotLibrary, BuilderPage, and GettingStarted, which is on the landing
// page. Nearly every screen in the app opened by waiting on it.
//
// Concurrently it is 3.2s, not the ~1s the arithmetic suggests: 1Claw
// throttles concurrent vault reads, so eight at once cost roughly three
// sequential ones no matter what this end does. That is why there is a
// cache above as well — the two together are what make the pages open
// quickly, and neither is sufficient alone.
//
// The reads are independent GETs against different paths, so there is no
// ordering to preserve between them, only in the output — which is why
// results go into a pre-sized slice by index rather than being appended as
// they finish.
func (s *Server) handleConnectionsStatus(w http.ResponseWriter, r *http.Request) {
	if s.OneClaw == nil || !s.OneClaw.Configured() {
		writeJSONCached(w, r, http.StatusOK, []connectionStatus{})
		return
	}
	statuses, err := s.connCache.do(connectionTTL, func() ([]connectionStatus, error) {
		vault, err := s.OneClaw.EnsureVault("nanobots-main")
		if err != nil {
			return nil, err
		}
		out := make([]connectionStatus, len(connectionServices))
		var wg sync.WaitGroup
		for i, service := range connectionServices {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.OneClaw.GetSecret(vault.ID, vaultKeyFor[service])
				connected := err == nil
				if service == "linkedin" && !connected {
					// LinkedIn may have connected without a refresh token at all
					// (see internal/step/linkedin_live.go) — a stored access token
					// alone still counts as connected.
					_, err := s.OneClaw.GetSecret(vault.ID, "linkedin/access_token")
					connected = err == nil
				}
				out[i] = connectionStatus{Service: service, Connected: connected}
			}()
		}
		wg.Wait()
		return out, nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSONCached(w, r, http.StatusOK, statuses)
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
	s.connCache.invalidate()
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
	s.connCache.invalidate()
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
	s.connCache.invalidate()
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
		s.connCache.invalidate()
	} else {
		if err := s.OneClaw.PutSecret(vault.ID, "linkedin/access_token", tr.AccessToken); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.connCache.invalidate()
	}
	writeJSON(w, http.StatusOK, connectionStatus{Service: "linkedin", Connected: true})
}

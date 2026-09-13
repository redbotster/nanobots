package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Webhook triggers: the third trigger type, declared since the first commit
// and never delivered.
//
// `trigger: {type: webhook}` has been in the schema, reported by
// /api/swarms, and documented as inert. lead-to-meeting is written around
// one — a website form submission arrives, a lead gets logged, enriched and
// routed — and the only way to run it was by hand with an example payload
// pasted into the swarm.
//
// The body becomes {{trigger.payload}} in every bot's input templates,
// which is why lead-to-meeting can now read
// `{{trigger.payload | default: vars.example_lead}}` and work both ways: a
// real form post when one arrives, its own example when someone presses
// Run.
//
// Token-authenticated, for the same reason the Shroud proxy is: this one
// starts runs, and runs send email. An unauthenticated URL that emails your
// customers is not a feature.

// WebhookTrigger holds the shared secret callers must present.
type WebhookTrigger struct {
	Token string
}

// LoadWebhookToken returns the webhook secret, creating it on first use.
// Same shape as the Shroud proxy's token, and stored beside it.
func LoadWebhookToken(stateDir string) (string, error) {
	return loadOrCreateToken(filepath.Join(stateDir, "webhook-token"))
}

func (t *WebhookTrigger) authorised(r *http.Request) bool {
	if t == nil || t.Token == "" {
		return false
	}
	// Header or query, because a webhook sender is often a form service
	// with no way to set headers. The query form is the weaker one and it
	// is the caller's choice, not a default.
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		got = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(t.Token)) == 1
}

// handleWebhook starts a run of the named swarm with the request body as
// its trigger payload.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhook == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("webhook triggers are not enabled on this daemon"))
		return
	}
	if !s.Webhook.authorised(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="nanobots-webhook"`)
		writeError(w, http.StatusUnauthorized, fmt.Errorf("this endpoint needs the webhook token"))
		return
	}

	name := r.PathValue("swarm")
	path, sw, err := s.swarmByName(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	// A swarm that didn't ask to be triggered this way isn't triggered
	// this way. Otherwise the token becomes a "run anything" key, and a
	// cron swarm could be fired at any hour by anyone holding it.
	if !strings.EqualFold(sw.Spec.Trigger.Type, "webhook") {
		writeError(w, http.StatusConflict, fmt.Errorf(
			"%s has trigger type %q, not webhook — add `trigger: {type: webhook}` to it if you meant this",
			name, sw.Spec.Trigger.Type))
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			// Refused rather than passed through as a string: a bot
			// declaring a json input would get something it cannot read,
			// and "your form posted form-encoded" is a better error than
			// a type failure three containers in.
			writeError(w, http.StatusBadRequest, fmt.Errorf("the body must be JSON: %w", err))
			return
		}
	}

	run, err := s.Orchestrator.ExecuteSwarmWithTrigger(path, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.Runs.Add(run)
	// 202: the run has started, not finished. A webhook sender should not
	// be held open for a swarm that opens an approval gate and waits half
	// an hour for a human.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id": run.ID,
		"swarm":  run.SwarmName,
		"status": run.GetStatus(),
	})
}

// swarmByName finds a swarm by its metadata name or its filename, so a
// caller can use whichever they know.
func (s *Server) swarmByName(name string) (string, *schema.Nanoswarm, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", nil, fmt.Errorf("invalid swarm name %q", name)
	}
	entries, err := os.ReadDir(s.swarmsDir())
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(s.swarmsDir(), e.Name())
		sw, err := schema.LoadNanoswarm(path)
		if err != nil {
			continue
		}
		if sw.Metadata.Name == name || strings.TrimSuffix(e.Name(), ".yaml") == name {
			return path, sw, nil
		}
	}
	return "", nil, fmt.Errorf("no swarm called %q", name)
}

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redbotster/nanobots/internal/schema"
)

func webhookServer(t *testing.T, swarms map[string]string) *Server {
	t.Helper()
	srv := testServer(t)
	dir := t.TempDir()
	srv.SwarmsDir = dir
	srv.Webhook = &WebhookTrigger{Token: "test-token"}
	for name, body := range swarms {
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return srv
}

const webhookSwarm = `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: intake
  description: probe
spec:
  trigger:
    type: webhook
  bots: []
  snaps: []
`

const cronSwarm = `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: nightly
  description: probe
spec:
  trigger:
    type: cron
    expr: "0 8 * * *"
  bots: []
  snaps: []
`

func post(srv *Server, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	return rec
}

// This endpoint starts runs, and runs send email. An unauthenticated URL
// that emails your customers is not a feature.
func TestAWebhookRefusesWithoutTheToken(t *testing.T) {
	srv := webhookServer(t, map[string]string{"intake": webhookSwarm})
	for _, tok := range []string{"", "wrong", "test-toke"} {
		if got := post(srv, "/webhooks/intake", tok, "{}").Code; got != http.StatusUnauthorized {
			t.Errorf("token %q -> %d, want 401", tok, got)
		}
	}
}

// A swarm that didn't ask to be triggered this way isn't. Otherwise the
// token becomes a "run anything" key and a nightly swarm could be fired at
// any hour by whoever holds it.
func TestOnlyAWebhookSwarmCanBeTriggered(t *testing.T) {
	srv := webhookServer(t, map[string]string{"nightly": cronSwarm})
	rec := post(srv, "/webhooks/nightly", "test-token", "{}")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cron") {
		t.Errorf("the refusal does not say what the trigger actually is: %s", rec.Body.String())
	}
}

func TestAnUnknownSwarmIsNotFound(t *testing.T) {
	srv := webhookServer(t, map[string]string{"intake": webhookSwarm})
	if got := post(srv, "/webhooks/nope", "test-token", "{}").Code; got != http.StatusNotFound {
		t.Errorf("status = %d, want 404", got)
	}
}

// A form-encoded body would arrive as a string where a bot declares a json
// input, and "your form posted form-encoded" is a better error than a type
// failure three containers in.
func TestANonJSONBodyIsRefusedUpFront(t *testing.T) {
	srv := webhookServer(t, map[string]string{"intake": webhookSwarm})
	rec := post(srv, "/webhooks/intake", "test-token", "name=Priya&company=Meridian")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "JSON") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// A swarm can be named by its metadata name or its filename, since a caller
// configuring a form service knows whichever they were shown.
func TestASwarmCanBeNamedEitherWay(t *testing.T) {
	srv := webhookServer(t, map[string]string{"lead-intake": webhookSwarm})
	for _, name := range []string{"intake", "lead-intake"} {
		if _, _, err := srv.swarmByName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, bad := range []string{"", "../etc/passwd", "a/b"} {
		if _, _, err := srv.swarmByName(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// Not enabled means not there, rather than a route that 401s and hints at
// a feature this daemon doesn't have.
func TestTheWebhookRouteIsAbsentUnlessEnabled(t *testing.T) {
	srv := webhookServer(t, map[string]string{"intake": webhookSwarm})
	srv.Webhook = nil
	if got := post(srv, "/webhooks/intake", "test-token", "{}").Code; got != http.StatusNotFound {
		t.Errorf("status = %d, want 404", got)
	}
}

// Both guarded endpoints must survive a restart, or everything configured
// against them breaks on the next reboot — which now happens automatically.
func TestTokensAreStableAndPrivate(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadWebhookToken(dir)
	if err != nil || len(first) < 32 {
		t.Fatalf("token = %q, err = %v", first, err)
	}
	again, _ := LoadWebhookToken(dir)
	if again != first {
		t.Error("token changed across restarts")
	}
	// And it is not the Shroud proxy's: two endpoints, two secrets.
	shroud, _ := LoadShroudProxyToken(dir)
	if shroud == first {
		t.Error("the webhook and Shroud tokens are the same secret")
	}
	info, err := os.Stat(filepath.Join(dir, "webhook-token"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

// The details endpoint is how anyone actually finds their URL. Before it,
// the only instruction was "cat ~/.nanobots/state/agents/webhook-token".
func TestWebhookDetailsGivesTheURLAndTheToken(t *testing.T) {
	srv := webhookServer(t, map[string]string{"intake": webhookSwarm})

	r := httptest.NewRequest(http.MethodGet, "/api/swarms/intake/webhook", nil)
	r.Host = "localhost:8848"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got WebhookDetails
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := "http://localhost:8848/webhooks/intake"; got.URL != want {
		t.Errorf("url = %q, want %q", got.URL, want)
	}
	if got.Token != "test-token" {
		t.Errorf("token = %q", got.Token)
	}
	// The curl line has to be runnable as-is, which means it carries the
	// token — the whole point is not having to assemble it by hand.
	for _, want := range []string{got.URL, got.Token, "Content-Type: application/json"} {
		if !strings.Contains(got.Curl, want) {
			t.Errorf("curl does not contain %q:\n%s", want, got.Curl)
		}
	}
}

// The URL is built from the Host the caller used, not from a hardcoded
// loopback address. Someone accepting real webhooks reaches this daemon
// through a tunnel, and a URL naming 127.0.0.1 is useless to them —
// which is precisely the case where they need it most.
func TestWebhookDetailsUsesTheHostTheCallerReachedItOn(t *testing.T) {
	srv := webhookServer(t, map[string]string{"intake": webhookSwarm})

	r := httptest.NewRequest(http.MethodGet, "/api/swarms/intake/webhook", nil)
	r.Host = "nanobots.example.ts.net"
	r.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)

	var got WebhookDetails
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := "https://nanobots.example.ts.net/webhooks/intake"; got.URL != want {
		t.Errorf("url = %q, want %q", got.URL, want)
	}
}

// Offering a posting URL for a swarm nothing would post to is worse than
// refusing: it looks configured.
func TestWebhookDetailsRefusesANonWebhookSwarm(t *testing.T) {
	srv := webhookServer(t, map[string]string{"nightly": cronSwarm})

	r := httptest.NewRequest(http.MethodGet, "/api/swarms/nightly/webhook", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cron") {
		t.Errorf("the refusal should name the trigger it actually has: %s", rec.Body.String())
	}
}

// A webhook swarm is no longer inert, and the summary must stop saying so
// — the card renders an apology off that field.
func TestAWebhookSwarmIsNoLongerReportedAsInert(t *testing.T) {
	var sum SwarmSummary
	describeSchedule(&sum, schema.Trigger{Type: "webhook", Expr: "website.form.submitted"}, time.Now())
	if sum.TriggerType != "webhook" {
		t.Errorf("trigger_type = %q", sum.TriggerType)
	}
	if sum.InertTrigger != "" {
		t.Errorf("inert_trigger = %q, but webhooks fire now", sum.InertTrigger)
	}

	// `event:` still is inert, and must keep saying so.
	var ev SwarmSummary
	describeSchedule(&ev, schema.Trigger{Type: "event", Expr: "drive.file.created"}, time.Now())
	if ev.InertTrigger != "drive.file.created" {
		t.Errorf("event inert_trigger = %q, want the expression", ev.InertTrigger)
	}
}

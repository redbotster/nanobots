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

	"gopkg.in/yaml.v3"

	"github.com/redbotster/nanobots/internal/schema"
)

func TestSetServiceConnectionInYAMLAgainstRealBotFiles(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range []struct {
		bot, service string
	}{
		{"receipt-filer", "gmail"},
		{"receipt-filer", "sheets"},
		{"post-publisher", "x"},
		{"post-publisher", "linkedin"},
	} {
		t.Run(tc.bot+"/"+tc.service, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, "bots", tc.bot, "nanobot.yaml"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			updated, err := setServiceConnectionInYAML(raw, tc.service, "oauth_native")
			if err != nil {
				t.Fatalf("setServiceConnectionInYAML: %v", err)
			}

			// Every other line must survive untouched — count matches and diff.
			origLines := strings.Split(string(raw), "\n")
			newLines := strings.Split(string(updated), "\n")
			if len(origLines) != len(newLines) {
				t.Fatalf("line count changed: %d -> %d", len(origLines), len(newLines))
			}
			changed := 0
			for i := range origLines {
				if origLines[i] != newLines[i] {
					changed++
					if !strings.Contains(newLines[i], "connection: oauth_native") {
						t.Errorf("unexpected changed line %d: %q -> %q", i, origLines[i], newLines[i])
					}
				}
			}
			if changed != 1 {
				t.Errorf("expected exactly 1 changed line, got %d", changed)
			}

			// The result must still be valid, loadable YAML with the right
			// service actually flipped and every other service untouched.
			var nb schema.Nanobot
			if err := yaml.Unmarshal(updated, &nb); err != nil {
				t.Fatalf("edited YAML doesn't parse: %v", err)
			}
			found := false
			for _, s := range nb.Spec.Services {
				if s.ID == tc.service {
					found = true
					if s.Connection != "oauth_native" {
						t.Errorf("service %s connection = %q, want oauth_native", tc.service, s.Connection)
					}
				} else if s.Connection != "demo" {
					t.Errorf("unrelated service %s connection changed to %q", s.ID, s.Connection)
				}
			}
			if !found {
				t.Fatalf("service %q not found after edit", tc.service)
			}
		})
	}
}

func TestSetServiceConnectionInYAMLRoundTripsBackToDemo(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "bots", "receipt-filer", "nanobot.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	live, err := setServiceConnectionInYAML(raw, "gmail", "oauth_native")
	if err != nil {
		t.Fatalf("set live: %v", err)
	}
	back, err := setServiceConnectionInYAML(live, "gmail", "demo")
	if err != nil {
		t.Fatalf("set demo: %v", err)
	}
	if string(back) != string(raw) {
		t.Errorf("round trip demo -> live -> demo didn't restore the original file byte for byte")
	}
}

func TestSetServiceConnectionInYAMLErrorsOnUnknownService(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "bots", "receipt-filer", "nanobot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setServiceConnectionInYAML(raw, "nonexistent", "oauth_native"); err == nil {
		t.Fatal("expected an error for a service id that doesn't exist")
	}
}

// --- HTTP handler tests ---

func testServerWithBotsCopy(t *testing.T) *Server {
	t.Helper()
	srv := testServerWithOneClaw(t)
	botsDir := t.TempDir()
	root := repoRoot(t)
	for _, id := range []string{"receipt-filer", "post-publisher"} {
		if err := copyDirForTest(filepath.Join(root, "bots", id), filepath.Join(botsDir, id)); err != nil {
			t.Fatalf("copy fixture bot: %v", err)
		}
	}
	srv.BotsDir = botsDir
	return srv
}

func TestHandleSetBotServiceConnectionRejectsWhenNotConnected(t *testing.T) {
	srv := testServerWithBotsCopy(t)
	body, _ := json.Marshal(setBotServiceConnectionRequest{Live: true})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/receipt-filer/services/gmail/connection", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSetBotServiceConnectionGoesLiveOnceConnected(t *testing.T) {
	srv := testServerWithBotsCopy(t)
	vault, err := srv.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	if err := srv.OneClaw.PutSecret(vault.ID, "google/refresh_token", "tok"); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}

	body, _ := json.Marshal(setBotServiceConnectionRequest{Live: true})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/receipt-filer/services/gmail/connection", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var bot BotSummary
	json.Unmarshal(rec.Body.Bytes(), &bot)
	var gmail *schema.Service
	for i := range bot.Services {
		if bot.Services[i].ID == "gmail" {
			gmail = &bot.Services[i]
		}
	}
	if gmail == nil || gmail.Connection != "oauth_native" {
		t.Fatalf("gmail service = %+v, want connection=oauth_native", gmail)
	}

	// And back to demo needs no connection at all.
	body, _ = json.Marshal(setBotServiceConnectionRequest{Live: false})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/receipt-filer/services/gmail/connection", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("revert status = %d, body: %s", rec.Code, rec.Body.String())
	}
}

// A swarm's ServicesLive count is cached (swarmInspectTTL, see
// inspectedSwarms) because computing it re-resolves every swarm's whole bot
// graph — but toggling a bot's own service connection must be reflected in
// that count the moment the toggle returns, not up to ten seconds later.
// Anything less is the "0/4 live, reading as everything is broken" bug
// CLAUDE.md documents, just delayed instead of permanent.
func TestSwarmsServicesLiveReflectsAConnectionToggleImmediately(t *testing.T) {
	srv := testServerWithBotsCopy(t)
	srv.SwarmsDir = t.TempDir()
	swarmYAML := `apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: uses-receipt-filer
spec:
  trigger: { type: manual }
  bots:
    - id: receipts
      use: receipt-filer@0.1.0
`
	if err := os.WriteFile(filepath.Join(srv.SwarmsDir, "uses-receipt-filer.yaml"), []byte(swarmYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	liveCountOf := func() int {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/swarms", nil))
		var swarms []SwarmSummary
		if err := json.Unmarshal(rec.Body.Bytes(), &swarms); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, s := range swarms {
			if s.Name == "uses-receipt-filer" {
				return s.ServicesLive
			}
		}
		t.Fatal("uses-receipt-filer not found in the list")
		return -1
	}

	if got := liveCountOf(); got != 0 {
		t.Fatalf("ServicesLive before connecting = %d, want 0 (demo)", got)
	}

	vault, err := srv.OneClaw.EnsureVault("nanobots-main")
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	if err := srv.OneClaw.PutSecret(vault.ID, "google/refresh_token", "tok"); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	body, _ := json.Marshal(setBotServiceConnectionRequest{Live: true})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/receipt-filer/services/gmail/connection", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	if got := liveCountOf(); got == 0 {
		t.Error("ServicesLive after connecting = 0, want > 0 — the swarmInspectCache was not invalidated by the connection toggle")
	}
}

func TestHandleSetBotServiceConnectionUnknownBot(t *testing.T) {
	srv := testServerWithBotsCopy(t)
	body, _ := json.Marshal(setBotServiceConnectionRequest{Live: false})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/nonexistent/services/gmail/connection", bytes.NewReader(body)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSetBotServiceConnectionUnknownService(t *testing.T) {
	srv := testServerWithBotsCopy(t)
	body, _ := json.Marshal(setBotServiceConnectionRequest{Live: false})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bots/receipt-filer/services/nonexistent/connection", bytes.NewReader(body)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

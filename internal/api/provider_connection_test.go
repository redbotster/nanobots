package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// A bot whose YAML carries comments and two services, one of them Google —
// the shape the bulk switch has to edit without wrecking anything else.
const googleBot = `apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: %s
  version: 0.1.0
spec:
  harness: { type: bare }
  services:
    # This comment explains why the service is here and must survive.
    - id: gmail
      provider: google
      scopes: [gmail.readonly]
      connection: demo
    - id: chat
      provider: slack
      connection: demo
  ports:
    outputs:
      - name: out
        type: string
  steps:
    - name: only
      type: transform.now
      output: out
`

func botsDirWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
		body := strings.Replace(googleBot, "%s", n, 1)
		if err := os.WriteFile(filepath.Join(dir, n, "nanobot.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBotsUsingProviderCountsWhatTheUIPromises(t *testing.T) {
	srv := &Server{BotsDir: botsDirWith(t, "alpha", "beta", "gamma")}

	got, err := srv.botsUsingProvider("google")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Demo) != 3 || len(got.Live) != 0 {
		t.Fatalf("got demo=%v live=%v, want all three on demo", got.Demo, got.Live)
	}
	// Sorted, because the UI names them and an unstable order would make
	// the list jump around between polls.
	if got.Demo[0] != "alpha" || got.Demo[2] != "gamma" {
		t.Errorf("demo = %v, want it sorted", got.Demo)
	}
	// A bot is counted once per provider, not once per service.
	if slack, _ := srv.botsUsingProvider("slack"); len(slack.Demo) != 3 {
		t.Errorf("slack demo = %v, want the same three bots", slack.Demo)
	}
}

func TestSetProviderOnBotSwitchesOnlyThatProvider(t *testing.T) {
	dir := botsDirWith(t, "alpha")
	srv := &Server{BotsDir: dir}
	path := filepath.Join(dir, "alpha", "nanobot.yaml")

	changed, err := srv.setProviderOnBot("alpha", "google", schema.ConnectionOAuthNative)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("reported no change, but gmail was on demo")
	}

	raw, _ := os.ReadFile(path)
	got := string(raw)
	if !strings.Contains(got, "connection: oauth_native") {
		t.Errorf("gmail was not switched:\n%s", got)
	}
	// The Slack service is a different provider and must be untouched.
	if strings.Count(got, "connection: demo") != 1 {
		t.Errorf("expected slack to stay on demo:\n%s", got)
	}
	// The surgical edit must not eat comments — this is the same YAML
	// editor the single-bot toggle uses.
	if !strings.Contains(got, "# This comment explains why") {
		t.Errorf("comment lost:\n%s", got)
	}
	// And the result still loads.
	nb, err := schema.LoadNanobot(path)
	if err != nil {
		t.Fatalf("edited bot no longer loads: %v", err)
	}
	for _, svc := range nb.Spec.Services {
		if svc.ID == "gmail" && svc.Connection != schema.ConnectionOAuthNative {
			t.Errorf("gmail connection = %q after reload", svc.Connection)
		}
	}
}

func TestSetProviderOnBotIsIdempotent(t *testing.T) {
	srv := &Server{BotsDir: botsDirWith(t, "alpha")}
	if _, err := srv.setProviderOnBot("alpha", "google", schema.ConnectionOAuthNative); err != nil {
		t.Fatal(err)
	}
	changed, err := srv.setProviderOnBot("alpha", "google", schema.ConnectionOAuthNative)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("reported a change on a second identical switch; the UI would claim it did something")
	}
}

// Going back to demo has to work on every bot, unconditionally — undoing
// should always be at least as easy as doing.
func TestSwitchingBackToDemoRestoresEveryBot(t *testing.T) {
	dir := botsDirWith(t, "alpha", "beta")
	srv := &Server{BotsDir: dir}

	for _, b := range []string{"alpha", "beta"} {
		if _, err := srv.setProviderOnBot(b, "google", schema.ConnectionOAuthNative); err != nil {
			t.Fatal(err)
		}
	}
	if live, _ := srv.botsUsingProvider("google"); len(live.Live) != 2 {
		t.Fatalf("setup failed: live = %v", live.Live)
	}

	for _, b := range []string{"alpha", "beta"} {
		if _, err := srv.setProviderOnBot(b, "google", schema.ConnectionDemo); err != nil {
			t.Fatal(err)
		}
	}
	back, _ := srv.botsUsingProvider("google")
	if len(back.Demo) != 2 || len(back.Live) != 0 {
		t.Errorf("after switching back: demo=%v live=%v", back.Demo, back.Live)
	}
}

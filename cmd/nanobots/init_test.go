package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/oneclaw"
)

func runInitWith(t *testing.T, envPath, input string) string {
	t.Helper()
	var out bytes.Buffer
	err := initSetup(initOptions{
		EnvPath:   envPath,
		In:        strings.NewReader(input),
		Out:       &out,
		NoBrowser: true,
	})
	if err != nil {
		t.Fatalf("initSetup: %v", err)
	}
	return out.String()
}

// Skipping every question must still leave a working install. Demo data is
// a real way to use this, not a degraded one, so the path that asks for
// nothing has to be one keypress away rather than an error.
func TestSkippingEverythingStillLeavesAWorkingInstall(t *testing.T) {
	env := filepath.Join(t.TempDir(), "nanobots.env")
	out := runInitWith(t, env, "3\n4\n")

	if !strings.Contains(out, "nanobots up") {
		t.Errorf("setup did not say how to start it:\n%s", out)
	}
	if !strings.Contains(out, "example data") {
		t.Errorf("setup did not say what happens without a key:\n%s", out)
	}
	if _, err := os.Stat(env); err == nil {
		if b, _ := os.ReadFile(env); strings.Contains(string(b), "ONECLAW_API_KEY") {
			t.Error("skipping wrote a 1Claw key anyway")
		}
	}
}

// Re-running setup on a configured machine must not be how someone loses a
// working install.
func TestItLeavesAnExistingKeyAlone(t *testing.T) {
	env := filepath.Join(t.TempDir(), "nanobots.env")
	if err := oneclaw.WriteEnvValue(env, "ONECLAW_API_KEY", "1ck_existing_key_value"); err != nil {
		t.Fatal(err)
	}

	out := runInitWith(t, env, "4\n")

	if !strings.Contains(out, "already configured") {
		t.Errorf("did not say the key was already there:\n%s", out)
	}
	body, _ := os.ReadFile(env)
	if !strings.Contains(string(body), "1ck_existing_key_value") {
		t.Error("the existing key was overwritten")
	}
	// And it must not print the key back in full.
	if strings.Contains(out, "1ck_existing_key_value") {
		t.Errorf("setup echoed the whole key to the terminal:\n%s", out)
	}
}

// The two key kinds are not equivalent, and choosing the narrower one is
// only possible if the difference is stated where the choice is made.
func TestItExplainsWhichKeyKindToUse(t *testing.T) {
	env := filepath.Join(t.TempDir(), "nanobots.env")
	out := runInitWith(t, env, "3\n4\n")

	for _, want := range []string{"ocv_", "1ck_", "Cannot install connectors"} {
		if !strings.Contains(out, want) {
			t.Errorf("the key-kind help does not mention %q:\n%s", want, out)
		}
	}
}

func TestAModelKeyIsSavedWhen1ClawIsSkipped(t *testing.T) {
	env := filepath.Join(t.TempDir(), "nanobots.env")
	out := runInitWith(t, env, "3\n1\nsk-ant-test-value\n")

	if !strings.Contains(out, "ANTHROPIC_API_KEY") {
		t.Errorf("did not report saving the model key:\n%s", out)
	}
	got, err := oneclaw.LoadEnvValue(env, "ANTHROPIC_API_KEY")
	if err != nil || got != "sk-ant-test-value" {
		t.Errorf("ANTHROPIC_API_KEY = %q (err %v), want it written", got, err)
	}
}

// A key that does not authenticate must not be written. It would fail at
// the first run of the first bot instead, a long way from the typo.
func TestABadKeyIsNotSaved(t *testing.T) {
	env := filepath.Join(t.TempDir(), "nanobots.env")
	orig := oneclaw.DefaultBaseURL
	oneclaw.DefaultBaseURL = "http://127.0.0.1:1" // nothing listens here
	defer func() { oneclaw.DefaultBaseURL = orig }()

	out := runInitWith(t, env, "2\n1ck_not_a_real_key\n4\n")

	if !strings.Contains(out, "did not authenticate") {
		t.Errorf("a bad key was accepted quietly:\n%s", out)
	}
	if got, _ := oneclaw.LoadEnvValue(env, "ONECLAW_API_KEY"); got != "" {
		t.Errorf("wrote an unauthenticated key: %q", got)
	}
}

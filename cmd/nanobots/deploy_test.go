package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A deploy with no flags runs the published image, and every default is
// decided here rather than somewhere only a live deploy would reveal.
//
// --image used to be required, because nothing was published and a guessed
// reference would have failed at pull time. v0.1.0 changed that.
func TestDeployDefaultsToThePublishedImage(t *testing.T) {
	got := withDeployDefaults(deployOptions{})
	if got.Image != DefaultImage {
		t.Errorf("image = %q, want the published one (%q)", got.Image, DefaultImage)
	}
	if !strings.HasPrefix(got.Image, "ghcr.io/redbotster/nanobots:v") {
		t.Errorf("the default image %q is not a pinned tag of this repo's own image — a runtime "+
			"that changes version under a running schedule is not a default", got.Image)
	}
	if got.Slug != "nanobots" || got.AgentName != "nanobots" || got.Environment != "production" {
		t.Errorf("defaults = %+v", got)
	}

	// What the user asked for always wins.
	asked := withDeployDefaults(deployOptions{Image: "ghcr.io/me/mine:v2", Slug: "s", AgentName: "a", Environment: "staging"})
	if asked.Image != "ghcr.io/me/mine:v2" || asked.Slug != "s" || asked.AgentName != "a" || asked.Environment != "staging" {
		t.Errorf("defaults overrode explicit flags: %+v", asked)
	}
}

// docs/1claw-feature-requests.md #11: 1Claw shipped a "binary" runtime
// template that runs a compiled program instead of pulling an image —
// --template opts out of the image default entirely, rather than sending
// both and leaving 1Claw to decide which startup mechanism wins.
func TestTemplateOptsOutOfTheImageDefault(t *testing.T) {
	got := withDeployDefaults(deployOptions{Template: "binary"})
	if got.Image != "" {
		t.Errorf("image = %q, want empty when a template is given", got.Image)
	}
	if got.Template != "binary" {
		t.Errorf("template = %q, want binary", got.Template)
	}

	// No template, no image: still the old default, unchanged.
	got = withDeployDefaults(deployOptions{})
	if got.Image != DefaultImage || got.Template != "" {
		t.Errorf("defaults = %+v, want the plain image default with no template", got)
	}
}

func TestBinaryURLFlagSetsTheNamedEnvVar(t *testing.T) {
	opts := deployOptions{AgentName: "nanobots", Environment: "production"}
	err := parseDeployFlags(&opts, []string{"--template", "binary", "--binary-url", "https://example.com/nanobots"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.RuntimeEnv["BINARY_URL"] != "https://example.com/nanobots" {
		t.Errorf("RuntimeEnv = %+v, want BINARY_URL set", opts.RuntimeEnv)
	}
}

func TestRuntimeEnvFlagIsRepeatableAndRejectsMissingEquals(t *testing.T) {
	opts := deployOptions{AgentName: "nanobots", Environment: "production"}
	err := parseDeployFlags(&opts, []string{
		"--runtime-env", "FOO=bar",
		"--runtime-env", "BAZ=qux",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.RuntimeEnv["FOO"] != "bar" || opts.RuntimeEnv["BAZ"] != "qux" {
		t.Errorf("RuntimeEnv = %+v, want both FOO and BAZ", opts.RuntimeEnv)
	}

	opts2 := deployOptions{}
	if err := parseDeployFlags(&opts2, []string{"--runtime-env", "no-equals-sign"}); err == nil {
		t.Error("expected an error for --runtime-env with no KEY=VALUE shape")
	}
}

// A deploy bills, so it must not get as far as creating anything when the
// machine has no credential at all.
//
// --env points at an empty temp file on purpose: without it this reads
// whatever ~/.secrets/nanobots.env the machine happens to have, so it passed
// on a laptop with a key and failed in CI without one. A test whose result
// depends on the developer's home directory is testing the developer.
func TestDeployRefusesWithoutACredential(t *testing.T) {
	err := runDeploy([]string{"1claw", "--env", emptyEnvFile(t)})
	if err == nil {
		t.Fatal("deploying with no 1Claw key should not be possible")
	}
	if !strings.Contains(err.Error(), "nanobots init") {
		t.Errorf("the refusal does not say what to do about it:\n%s", err)
	}
}

// emptyEnvFile is a dotenv with nothing in it, so a command under test reads
// a known-empty configuration instead of the machine's.
func emptyEnvFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nanobots.env")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDeployRejectsAnUnknownTarget(t *testing.T) {
	if err := runDeploy([]string{"heroku"}); err == nil {
		t.Error("an unknown deploy target should be refused")
	}
	if err := runDeploy(nil); err == nil {
		t.Error("deploy with no target should be refused")
	}
}

func TestDeployRejectsUnknownFlags(t *testing.T) {
	err := runDeploy([]string{"1claw", "--image", "x", "--public"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("err = %v, want an unknown-flag refusal", err)
	}
}

// A flag that takes a value must not silently swallow the next flag when
// its value is missing.
func TestDeployRejectsAFlagMissingItsValue(t *testing.T) {
	err := runDeploy([]string{"1claw", "--image"})
	if err == nil || !strings.Contains(err.Error(), "requires a value") {
		t.Errorf("err = %v, want a missing-value refusal", err)
	}
}

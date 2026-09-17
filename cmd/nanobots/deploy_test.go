package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A deploy bills. The command must refuse rather than guess when it has not
// been told what to run, and must never reach the point of creating
// anything on a bad flag.
//
// --env points at an empty temp file on purpose: without it this reads
// whatever ~/.secrets/nanobots.env the machine happens to have, so it passed
// on a laptop with a key and failed in CI without one. A test whose result
// depends on the developer's home directory is testing the developer.
func TestDeployRefusesWithoutAnImage(t *testing.T) {
	err := runDeploy([]string{"1claw", "--env", emptyEnvFile(t)})
	if err == nil {
		t.Fatal("deploying with no image should not be possible")
	}
	for _, want := range []string{"--image is required", "no `nanobots` runtime template"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not explain %q:\n%s", want, err)
		}
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

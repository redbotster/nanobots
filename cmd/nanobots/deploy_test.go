package main

import (
	"strings"
	"testing"
)

// A deploy bills. The command must refuse rather than guess when it has not
// been told what to run, and must never reach the point of creating
// anything on a bad flag.
func TestDeployRefusesWithoutAnImage(t *testing.T) {
	err := runDeploy([]string{"1claw"})
	if err == nil {
		t.Fatal("deploying with no image should not be possible")
	}
	for _, want := range []string{"--image is required", "no `nanobots` runtime template"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not explain %q:\n%s", want, err)
		}
	}
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

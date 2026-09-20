package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The normal dev loop: cwd is a checkout, bots/ is right there, and the
// fallback (which would extract this binary's own catalog) must never even
// be asked.
func TestResolveRootUsesTheCheckoutWhenBotsDirExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bots"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, botsDir, err := resolveRoot(dir, func() (string, error) {
		t.Fatal("fallback was called even though bots/ exists")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Errorf("root = %q, want %q", root, dir)
	}
	if want := filepath.Join(dir, "bots"); botsDir != want {
		t.Errorf("botsDir = %q, want %q", botsDir, want)
	}
}

// A released binary run from anywhere else: no bots/ at cwd, so the
// fallback's answer becomes both RepoRoot and where botsDir is derived
// from — exactly the layout a real checkout has.
func TestResolveRootFallsBackWhenNoBotsDirExists(t *testing.T) {
	dir := t.TempDir() // no bots/ subdirectory
	fallbackRoot := t.TempDir()
	root, botsDir, err := resolveRoot(dir, func() (string, error) {
		return fallbackRoot, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if root != fallbackRoot {
		t.Errorf("root = %q, want the fallback's %q", root, fallbackRoot)
	}
	if want := filepath.Join(fallbackRoot, "bots"); botsDir != want {
		t.Errorf("botsDir = %q, want %q", botsDir, want)
	}
}

// A fallback that also fails (no catalog built into this binary, and no
// bots/ here either) has to say both things, not just the second one —
// otherwise the error reads as "the catalog is broken" when the actual,
// fixable problem is "you're not in a nanobots checkout".
func TestResolveRootNamesBothFailuresWhenTheFallbackAlsoFails(t *testing.T) {
	dir := t.TempDir()
	_, _, err := resolveRoot(dir, func() (string, error) {
		return "", errors.New("no catalog built into this binary")
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"bots/", dir, "no catalog built into this binary"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

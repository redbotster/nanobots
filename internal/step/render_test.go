package step

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRenderHTML(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "t.html")
	if err := os.WriteFile(tmplPath, []byte("hello {{.name}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := RenderHTML(tmplPath, map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if string(out) != "hello world" {
		t.Errorf("RenderHTML = %q", out)
	}
}

func TestRenderHTMLToPDFNoChromeFallsBackToHTML(t *testing.T) {
	t.Setenv("NANOBOTS_CHROME_PATH", "/no/such/binary")
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "t.html")
	os.WriteFile(tmplPath, []byte("hi"), 0o600)
	data, mime, err := RenderHTMLToPDF(tmplPath, nil)
	if err != nil {
		t.Fatalf("RenderHTMLToPDF: %v", err)
	}
	if mime != "text/html" || string(data) != "hi" {
		t.Errorf("got %q, %q", mime, data)
	}
}

func TestRenderHTMLToPNGNoChromeFallsBackToHTML(t *testing.T) {
	t.Setenv("NANOBOTS_CHROME_PATH", "/no/such/binary")
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "t.html")
	os.WriteFile(tmplPath, []byte("hi"), 0o600)
	data, mime, err := RenderHTMLToPNG(tmplPath, nil)
	if err != nil {
		t.Fatalf("RenderHTMLToPNG: %v", err)
	}
	if mime != "text/html" || string(data) != "hi" {
		t.Errorf("got %q, %q", mime, data)
	}
}

func TestRenderHTMLToPDFTimesOutOnAHungChrome(t *testing.T) {
	// A fake "chrome" that hangs *and spawns a child that outlives it*,
	// which is what real Chrome does — renderer and GPU processes.
	//
	// The child matters. exec.CommandContext kills the process it started;
	// Wait then blocks until every pipe is closed, and a grandchild still
	// holds them. This script used to be a plain `sleep 10`, which macOS
	// exec's in place — so the shell *was* the sleep, killing it worked, and
	// the test passed here for as long as it existed. On Linux the shell and
	// the sleep are two processes and the call took the full ten seconds
	// against a 100ms timeout. CI found it on its first run.
	//
	// `sleep & wait` makes that two processes rather than one. It still does
	// not reproduce the hang on macOS — checked by removing WaitDelay again,
	// which leaves this passing in 0.10s here — so Linux CI is what actually
	// exercises the fix. Said plainly because a test that only bites on one
	// platform is worth knowing about before trusting a green run locally.
	dir := t.TempDir()
	fakeChrome := filepath.Join(dir, "chrome-that-hangs.sh")
	os.WriteFile(fakeChrome, []byte("#!/bin/sh\nsleep 10 &\nwait\n"), 0o755)
	t.Setenv("NANOBOTS_CHROME_PATH", fakeChrome)

	old := chromeRenderTimeout
	chromeRenderTimeout = 100 * time.Millisecond
	defer func() { chromeRenderTimeout = old }()

	tmplPath := filepath.Join(dir, "t.html")
	os.WriteFile(tmplPath, []byte("hi"), 0o600)

	start := time.Now()
	_, _, err := RenderHTMLToPDF(tmplPath, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a timeout error from a hung chrome process")
	}
	// Generous enough for chromeWaitDelay, tight enough to catch a return
	// that waited on the grandchild instead.
	if elapsed > chromeWaitDelay+3*time.Second {
		t.Errorf("RenderHTMLToPDF took %s against a %s timeout — it waited on a "+
			"child of the process it killed", elapsed, chromeRenderTimeout)
	}
}

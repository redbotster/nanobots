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
	// A fake "chrome" that just sleeps — proves a hung renderer fails fast
	// with a clear error instead of hanging the whole bot run.
	dir := t.TempDir()
	fakeChrome := filepath.Join(dir, "chrome-that-hangs.sh")
	os.WriteFile(fakeChrome, []byte("#!/bin/sh\nsleep 10\n"), 0o755)
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
	if elapsed > 5*time.Second {
		t.Errorf("RenderHTMLToPDF took %s, expected it to fail fast on timeout", elapsed)
	}
}

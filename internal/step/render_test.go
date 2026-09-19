package step

import (
	"os"
	"path/filepath"
	"strings"
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

// A remote asset an LLM-authored summary could carry (an <img src>, chosen
// by whoever's email or ticket the summary was built from) is fetched by
// this same process's Chrome, not by a callback network_egress already
// checks — so Chrome itself has to be pointed at the run's egress proxy
// when one is set.
// writeArgvRecordingChrome writes a fake "chrome" that logs its full argv
// to argvPath and touches whichever file --print-to-pdf= or --screenshot=
// names, so RenderHTMLToPDF/PNG's own read of the output succeeds
// regardless of where the proxy flag lands among the others.
func writeArgvRecordingChrome(t *testing.T, path, argvPath string) {
	t.Helper()
	script := `#!/bin/sh
echo "$@" > "` + argvPath + `"
for a in "$@"; do
  case "$a" in
    --print-to-pdf=*) touch "${a#--print-to-pdf=}" ;;
    --screenshot=*) touch "${a#--screenshot=}" ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A remote asset an LLM-authored summary could carry (an <img src>, chosen
// by whoever's email or ticket the summary was built from) is fetched by
// this same process's Chrome, not by a callback network_egress already
// checks — so Chrome itself has to be pointed at the run's egress proxy
// when one is set.
func TestRenderHTMLToPDFPassesTheEgressProxyToChrome(t *testing.T) {
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	fakeChrome := filepath.Join(dir, "chrome-records-argv.sh")
	writeArgvRecordingChrome(t, fakeChrome, argvPath)
	t.Setenv("NANOBOTS_CHROME_PATH", fakeChrome)
	t.Setenv("NANOBOTS_EGRESS_PROXY", "host.docker.internal:54321")

	tmplPath := filepath.Join(dir, "t.html")
	os.WriteFile(tmplPath, []byte("hi"), 0o600)

	if _, _, err := RenderHTMLToPDF(tmplPath, nil); err != nil {
		t.Fatalf("RenderHTMLToPDF: %v", err)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	if !strings.Contains(string(argv), "--proxy-server=host.docker.internal:54321") {
		t.Errorf("chrome invoked without --proxy-server: %s", argv)
	}
}

func TestRenderHTMLToPDFOmitsProxyFlagWhenNoneIsSet(t *testing.T) {
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	fakeChrome := filepath.Join(dir, "chrome-records-argv.sh")
	writeArgvRecordingChrome(t, fakeChrome, argvPath)
	t.Setenv("NANOBOTS_CHROME_PATH", fakeChrome)

	tmplPath := filepath.Join(dir, "t.html")
	os.WriteFile(tmplPath, []byte("hi"), 0o600)

	if _, _, err := RenderHTMLToPDF(tmplPath, nil); err != nil {
		t.Fatalf("RenderHTMLToPDF: %v", err)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	if strings.Contains(string(argv), "--proxy-server") {
		t.Errorf("chrome invoked with a proxy flag nobody set: %s", argv)
	}
}

// The PNG path (sheet-reporter's chart) needs the same treatment as PDF.
func TestRenderHTMLToPNGPassesTheEgressProxyToChrome(t *testing.T) {
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	fakeChrome := filepath.Join(dir, "chrome-records-argv.sh")
	writeArgvRecordingChrome(t, fakeChrome, argvPath)
	t.Setenv("NANOBOTS_CHROME_PATH", fakeChrome)
	t.Setenv("NANOBOTS_EGRESS_PROXY", "host.docker.internal:54321")

	tmplPath := filepath.Join(dir, "t.html")
	os.WriteFile(tmplPath, []byte("hi"), 0o600)

	if _, _, err := RenderHTMLToPNG(tmplPath, nil); err != nil {
		t.Fatalf("RenderHTMLToPNG: %v", err)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	if !strings.Contains(string(argv), "--proxy-server=host.docker.internal:54321") {
		t.Errorf("chrome invoked without --proxy-server: %s", argv)
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

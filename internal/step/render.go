package step

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// chromeRenderTimeout bounds the headless Chrome subprocess. Without this, a
// hung Chrome process (a real failure mode — a busy host, a crash-looping
// renderer, a stuck GPU process) would hang the whole bot run indefinitely
// instead of failing with a clear error. Comfortably under a bot's own
// max_runtime_secs guardrail (240s for recap-emails-to-pdf). A var, not a
// const, so tests can shrink it rather than waiting out the real timeout.
var chromeRenderTimeout = 30 * time.Second

// chromeWaitDelay bounds how long the timeout waits for a killed Chrome's
// children to let go.
//
// exec.CommandContext kills the process it started, and then Wait blocks
// until every pipe it handed out is closed — which a grandchild still holds.
// Chrome spawns renderer and GPU processes, so "timed out after 30s" could
// take considerably longer than 30s to actually return, which is the one
// thing a timeout exists to prevent.
//
// Found by CI rather than here: the hung-Chrome test stubs a shell script
// that sleeps, and on Linux the shell is killed while the sleep it spawned
// keeps the pipes open. The test took the full 10 seconds against a 100ms
// timeout. macOS happened to exec the sleep in place, so it passed locally
// and had done for as long as the test existed.
//
// WaitDelay closes the pipes and force-kills the group after the context is
// done, so the deadline is the deadline.
const chromeWaitDelay = 2 * time.Second

// RenderHTML executes an html/template file against data, returning the
// rendered HTML bytes.
func RenderHTML(templatePath string, data any) ([]byte, error) {
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", templatePath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template %s: %w", templatePath, err)
	}
	return buf.Bytes(), nil
}

// RenderHTMLToPDF renders templatePath to HTML then to PDF via a headless
// Chromium/Chrome binary (--headless --print-to-pdf). This is the real
// implementation the blueprint's §4 #5 gap calls for running in-container;
// outside a container (e.g. a local `nanobots conform` run) it uses whatever
// Chrome/Chromium it can find on the host. If none is found, it returns the
// plain HTML bytes instead of failing outright — conformance only checks
// that a `file` port was produced, not that it's byte-for-byte a PDF.
func RenderHTMLToPDF(templatePath string, data any) (pdfBytes []byte, mime string, err error) {
	html, err := RenderHTML(templatePath, data)
	if err != nil {
		return nil, "", err
	}
	chrome := findChrome()
	if chrome == "" {
		return html, "text/html", nil
	}

	dir, err := os.MkdirTemp("", "nanobots-render-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	htmlPath := filepath.Join(dir, "in.html")
	pdfPath := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(htmlPath, html, 0o600); err != nil {
		return nil, "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), chromeRenderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		// Chrome's default profile location lives under $HOME, which is
		// read-only when the container runs with --read-only (the whole
		// point of the harness images). Point it at the same tmpfs-backed
		// scratch dir we already made for the HTML/PDF files.
		"--user-data-dir="+dir,
		"--print-to-pdf="+pdfPath,
		"--no-pdf-header-footer",
		"file://"+htmlPath,
	)
	cmd.WaitDelay = chromeWaitDelay
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, "", fmt.Errorf("headless chrome render timed out after %s", chromeRenderTimeout)
	}
	if err != nil {
		return nil, "", fmt.Errorf("headless chrome render failed: %w (%s)", err, string(out))
	}
	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, "", fmt.Errorf("read rendered pdf: %w", err)
	}
	return pdf, "application/pdf", nil
}

// RenderHTMLToPNG is RenderHTMLToPDF's screenshot twin — same headless
// Chrome subprocess, same host-Chrome-optional fallback, just
// `--screenshot` instead of `--print-to-pdf`. Used for bots/sheet-reporter's
// chart_png output: a real, if simple, rasterized chart rather than an SVG
// or a PDF pretending to be one.
func RenderHTMLToPNG(templatePath string, data any) (pngBytes []byte, mime string, err error) {
	html, err := RenderHTML(templatePath, data)
	if err != nil {
		return nil, "", err
	}
	chrome := findChrome()
	if chrome == "" {
		return html, "text/html", nil
	}

	dir, err := os.MkdirTemp("", "nanobots-render-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	htmlPath := filepath.Join(dir, "in.html")
	pngPath := filepath.Join(dir, "out.png")
	if err := os.WriteFile(htmlPath, html, 0o600); err != nil {
		return nil, "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), chromeRenderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--user-data-dir="+dir,
		"--screenshot="+pngPath,
		"--window-size=640,480",
		"file://"+htmlPath,
	)
	cmd.WaitDelay = chromeWaitDelay
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, "", fmt.Errorf("headless chrome render timed out after %s", chromeRenderTimeout)
	}
	if err != nil {
		return nil, "", fmt.Errorf("headless chrome render failed: %w (%s)", err, string(out))
	}
	png, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, "", fmt.Errorf("read rendered png: %w", err)
	}
	return png, "image/png", nil
}

// findChrome locates a Chrome/Chromium binary. NANOBOTS_CHROME_PATH, when
// set, is authoritative — an operator who names a binary explicitly and
// finds nothing there almost certainly wants an error, not this function
// silently discovering a different Chrome elsewhere (which is also what
// keeps this function's behavior testable without a real Chrome install).
func findChrome() string {
	if p, isSet := os.LookupEnv("NANOBOTS_CHROME_PATH"); isSet {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	candidates := map[string][]string{
		"darwin": {"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
		"linux":  {"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"},
	}
	for _, p := range candidates[runtime.GOOS] {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

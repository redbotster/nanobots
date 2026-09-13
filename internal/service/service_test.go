package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cfg(root string) Config {
	return Config{
		Binary:   filepath.Join(root, "bin", "nanobots"),
		RepoRoot: root,
		Addr:     "127.0.0.1:7474",
		LogDir:   filepath.Join(root, "logs"),
	}
}

// A malformed plist doesn't error — launchd just never loads the job, and
// the automations quietly don't happen. So this is checked with macOS's own
// parser rather than by eye.
func TestThePlistIsValidToMacOSItself(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil not available")
	}
	body, err := Plist(cfg("/Users/someone/nanobots"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "job.plist")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
		t.Fatalf("plutil rejected it: %v\n%s\n%s", err, out, body)
	}
}

// The working directory is not decoration: nanobotd resolves bots/ and
// examples/swarms/ relative to it, so a job with the wrong one starts
// cleanly and then finds nothing to run.
func TestTheJobCarriesEverythingNanobotdNeeds(t *testing.T) {
	body, err := Plist(cfg("/Users/someone/nanobots"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>Label</key>",
		Label,
		"/Users/someone/nanobots/bin/nanobots",
		"<key>WorkingDirectory</key>",
		"<string>/Users/someone/nanobots</string>",
		"127.0.0.1:7474",
		// At login and kept alive: a scheduler that stops on the first
		// crash is one nobody can rely on.
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		// A background service with nowhere to write its log is one
		// nobody can debug.
		"nanobotd.log",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plist is missing %q:\n%s", want, body)
		}
	}
}

// Generated through encoding/xml rather than a format string, so a path
// with an ampersand produces a valid file instead of a job that silently
// never loads.
func TestAPathWithXMLInItDoesNotBreakTheFile(t *testing.T) {
	body, err := Plist(cfg(`/Users/a&b/"nano"bots`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, `<string>/Users/a&b/`) {
		t.Error("a raw ampersand reached the file")
	}
	if !strings.Contains(body, "&amp;") {
		t.Errorf("the ampersand was not escaped:\n%s", body)
	}
	if _, err := exec.LookPath("plutil"); err == nil {
		path := filepath.Join(t.TempDir(), "job.plist")
		_ = os.WriteFile(path, []byte(body), 0o644)
		if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
			t.Fatalf("plutil rejected an escaped path: %v\n%s", err, out)
		}
	}
}

// Install, see it, remove it — and removing something that isn't there is
// not an error, because "make sure it's gone" is a reasonable thing to run
// twice.
func TestInstallStatusRemove(t *testing.T) {
	if !Supported() {
		t.Skip("macOS only")
	}
	home := t.TempDir()
	if _, ok := Installed(home); ok {
		t.Fatal("reported installed before anything was written")
	}
	path, err := Write(home, cfg(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := Installed(home); !ok || got != path {
		t.Errorf("not found after writing: %q %v", got, ok)
	}
	if _, err := Remove(home); err != nil {
		t.Fatal(err)
	}
	if _, ok := Installed(home); ok {
		t.Error("still installed after removal")
	}
	if _, err := Remove(home); err != nil {
		t.Errorf("removing twice should be fine: %v", err)
	}
}

// It writes into ~/Library/LaunchAgents, not /Library/LaunchDaemons.
// nanobotd holds one person's credentials and runs their automations; it
// has no business running as root for everyone on the machine.
func TestItIsAPerUserAgentNotASystemDaemon(t *testing.T) {
	p := PlistPath("/Users/someone")
	if !strings.Contains(p, "/Users/someone/Library/LaunchAgents/") {
		t.Errorf("path = %q", p)
	}
	if strings.Contains(p, "LaunchDaemons") {
		t.Errorf("installs system-wide: %q", p)
	}
}

func TestAJobNeedsABinaryAndARoot(t *testing.T) {
	if _, err := Plist(Config{RepoRoot: "/x"}); err == nil {
		t.Error("accepted a job with no binary")
	}
	if _, err := Plist(Config{Binary: "/x/nanobots"}); err == nil {
		t.Error("accepted a job with no working directory")
	}
}

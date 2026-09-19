package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRootForWarmImages(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if _, err := os.Stat(filepath.Join(root, "bots")); err != nil {
		t.Fatalf("wrong repo root %s: %v", root, err)
	}
	return root
}

// Against the real catalog: everything that needs a container needs one of
// exactly two images, bare or openclaw — llm merges into bare (it's a
// callback, not a runtime), and nothing declares a harness this build
// doesn't build.
func TestHarnessTypesInUseAgainstTheRealCatalog(t *testing.T) {
	root := repoRootForWarmImages(t)
	types := harnessTypesInUse(filepath.Join(root, "bots"))
	if len(types) == 0 {
		t.Fatal("no harness types found in the real catalog")
	}
	for _, ty := range types {
		if ty != "bare" && ty != "openclaw" {
			t.Errorf("unexpected harness type %q — only bare and openclaw are built", ty)
		}
	}
	if !containsStr(types, "openclaw") {
		t.Error("the real catalog has render-to-pdf bots; openclaw should be in the list")
	}
}

func writeWarmBot(t *testing.T, botsDir, name, extra string) {
	t.Helper()
	dir := filepath.Join(botsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: ` + name + `
  version: 0.1.0
  description: probe
spec:
  harness:
    type: bare
` + extra
	if err := os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A bot whose steps never touch a browser or an LLM callback in a way that
// needs a container runs in-process — nothing to warm an image for.
func TestHarnessTypesInUseSkipsInProcessBots(t *testing.T) {
	botsDir := t.TempDir()
	writeWarmBot(t, botsDir, "local-only", `  steps:
    - name: noop
      type: transform.now
`)
	types := harnessTypesInUse(botsDir)
	if len(types) != 0 {
		t.Errorf("types = %v, want none — this bot never touches Docker", types)
	}
}

// A bot declared bare but rendering to pdf/png is promoted to openclaw at
// run time (imageFor) — the warm-up has to agree, or it builds the image
// nothing actually needs while leaving the one that's needed cold.
func TestHarnessTypesInUsePromotesARenderingBareBotToOpenclaw(t *testing.T) {
	botsDir := t.TempDir()
	writeWarmBot(t, botsDir, "renders-pdf", `  steps:
    - name: draw
      type: transform.render
      to: pdf
`)
	types := harnessTypesInUse(botsDir)
	if len(types) != 1 || types[0] != "openclaw" {
		t.Errorf("types = %v, want exactly [openclaw]", types)
	}
}

// Rendering to plain html needs no browser at all — html/template is Go
// stdlib, not Chrome — so a bare bot doing only that runs in-process, the
// same as any other bare bot with nothing that needs a container. Nothing
// to warm an image for.
func TestHarnessTypesInUseDoesNotPromoteAPlainHTMLRender(t *testing.T) {
	botsDir := t.TempDir()
	writeWarmBot(t, botsDir, "renders-html", `  steps:
    - name: draw
      type: transform.render
      to: html
`)
	types := harnessTypesInUse(botsDir)
	if len(types) != 0 {
		t.Errorf("types = %v, want none — this bot runs in-process", types)
	}
}

// A bot declaring the openclaw harness directly, with no render step at
// all — needs a container (runsInProcess refuses any declared-openclaw
// bot outright), and imageFor leaves it on openclaw since it isn't a
// browser-needing bot being demoted, it's already declared as one.
func TestHarnessTypesInUseCountsADeclaredOpenclawBot(t *testing.T) {
	botsDir := t.TempDir()
	dir := filepath.Join(botsDir, "declared-openclaw")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte(`apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: declared-openclaw
  version: 0.1.0
  description: probe
spec:
  harness:
    type: openclaw
  steps:
    - name: noop
      type: transform.now
`), 0o600)
	types := harnessTypesInUse(botsDir)
	if len(types) != 1 || types[0] != "bare" {
		t.Errorf("types = %v, want exactly [bare] — imageFor demotes a declared-openclaw bot that never renders", types)
	}
}

// A bad nanobot.yaml is skipped rather than failing the whole scan.
func TestHarnessTypesInUseSkipsABadBotDirectory(t *testing.T) {
	botsDir := t.TempDir()
	os.MkdirAll(filepath.Join(botsDir, "broken"), 0o755)
	os.WriteFile(filepath.Join(botsDir, "broken", "nanobot.yaml"), []byte("not: yaml: at: all: ["), 0o600)
	writeWarmBot(t, botsDir, "renders-pdf", `  steps:
    - name: draw
      type: transform.render
      to: pdf
`)
	types := harnessTypesInUse(botsDir)
	if len(types) != 1 || types[0] != "openclaw" {
		t.Errorf("types = %v, want the good bot's type despite the broken directory", types)
	}
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file: internal/memory/honcho_deployment_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// docker/honcho/config.toml has to name gemini in every model_config
// section it defines, because Honcho defaults each section to
// openai/gpt-5.4-mini *independently* — there is no global provider
// switch. Miss one and the server demands an OpenAI key at startup, or
// worse, starts and fails only when that one feature is first used.
//
// This is a shipped asset someone will edit (to add a section, to change a
// model), so the invariant is worth stating out loud rather than leaving in
// the file's header comment.
func TestShippedHonchoConfigNamesGeminiInEveryModelSection(t *testing.T) {
	path := filepath.Join(repoRoot(t), "docker", "honcho", "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var section string
	var sections, offenders []string
	sawTransport := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			if strings.HasSuffix(section, "model_config") {
				sections = append(sections, section)
			}
			continue
		}
		if !strings.HasSuffix(section, "model_config") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "transport" {
			continue
		}
		sawTransport[section] = true
		if got := strings.Trim(strings.TrimSpace(value), `"`); got != "gemini" {
			offenders = append(offenders, section+" = "+got)
		}
	}

	if len(sections) == 0 {
		t.Fatal("no model_config sections found — did the config move?")
	}
	for _, s := range sections {
		if !sawTransport[s] {
			offenders = append(offenders, s+" names no transport, so it silently defaults to openai")
		}
	}
	if len(offenders) > 0 {
		t.Errorf("docker/honcho/config.toml has model sections that are not gemini:\n  %s\n"+
			"Honcho resolves the provider per section, so one of these makes the whole"+
			" server ask for an OpenAI key. Add transport = \"gemini\".",
			strings.Join(offenders, "\n  "))
	}
}

// The port the compose file publishes and the HONCHO_URL the docs tell
// people to set are the same fact written in two places. They were, once.
func TestHonchoDocsAndComposeAgreeOnThePort(t *testing.T) {
	root := repoRoot(t)
	compose, err := os.ReadFile(filepath.Join(root, "docker", "honcho", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "127.0.0.1:8000:8000") {
		t.Error("the compose file no longer publishes Honcho on 8000")
	}
	docs, err := os.ReadFile(filepath.Join(root, "docs", "memory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docs), "http://localhost:8000") {
		t.Error("docs/memory.md no longer tells people to point HONCHO_URL at localhost:8000")
	}
}

// The start script must not be the place a credential ends up. It reads the
// key from ~/.secrets/nanobots.env and exports it; anything that writes it
// to disk beside the checkout (an .env, a generated compose) would put a
// key somewhere a later `git add -A` could pick it up.
func TestHonchoStartScriptDoesNotPersistTheKey(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docker", "honcho", "honcho.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if !strings.Contains(script, ".secrets/nanobots.env") {
		t.Error("the script no longer sources the one place credentials live")
	}
	for _, bad := range []string{"> .env", ">> .env", "tee .env"} {
		if strings.Contains(script, bad) {
			t.Errorf("the script writes a credential to disk (%q)", bad)
		}
	}
	// The checkout it clones must stay out of git.
	ignore, err := os.ReadFile(filepath.Join(repoRoot(t), "docker", "honcho", ".gitignore"))
	if err != nil || !strings.Contains(string(ignore), "src-checkout") {
		t.Error("docker/honcho/.gitignore does not exclude the cloned checkout")
	}
}

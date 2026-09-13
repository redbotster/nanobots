package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
	"github.com/redbotster/nanobots/internal/step"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file: internal/contract/conform_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// allBotIDs discovers every bot under bots/<id>/nanobot.yaml, so this test
// covers new bricks automatically instead of relying on someone remembering
// to add them to a hardcoded list.
func allBotIDs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "bots", e.Name(), "nanobot.yaml")); err == nil {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		t.Fatal("no bots found under bots/ — did the test find the wrong repo root?")
	}
	return ids
}

func TestRunConformanceOnLaunchBots(t *testing.T) {
	root := repoRoot(t)
	bots := allBotIDs(t, root)
	for _, id := range bots {
		id := id
		t.Run(id, func(t *testing.T) {
			report, err := RunConformance(filepath.Join(root, "bots", id), "")
			if err != nil {
				t.Fatalf("RunConformance: %v", err)
			}
			if !report.OK() {
				t.Fatalf("bot %s failed conformance:\n%s", id, report.String())
			}
			if len(report.Outputs) == 0 {
				t.Errorf("bot %s: expected some outputs, got none", id)
			}
		})
	}
}

// TestRunConformanceSeedsRealBlobContentForFileInputs proves fixtures/<port>.content
// actually reaches an ai.generate prompt as real text, not a blob reference —
// what bots/repurposer's conformance test depends on.
func TestRunConformanceSeedsRealBlobContentForFileInputs(t *testing.T) {
	dir := t.TempDir()
	nanobotYAML := `
apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: content-echo-test
  version: 0.1.0
spec:
  harness: { type: bare }
  ports:
    inputs:
      - name: source
        type: file
        required: true
    outputs:
      - name: echoed
        type: json
  steps:
    - name: echo
      type: ai.generate
      prompt_file: ./prompt.md
      inputs: { source: "{{inputs.source}}" }
      output: echoed
`
	if err := os.WriteFile(filepath.Join(dir, "nanobot.yaml"), []byte(nanobotYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("SOURCE:{{source}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixturesDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixturesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "inputs.json"), []byte(`{"source": {"uri": "nbf://placeholder", "mime": "text/plain"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "source.content"), []byte("the real transcript text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "ai.generate.json"), []byte(`{"ok": true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := RunConformance(dir, "")
	if err != nil {
		t.Fatalf("RunConformance: %v", err)
	}
	if !report.OK() {
		t.Fatalf("conformance failed:\n%s", report.String())
	}
}

func TestRunConformanceMissingInputsFixtureErrors(t *testing.T) {
	root := repoRoot(t)
	_, err := RunConformance(filepath.Join(root, "bots", "email-drive-file"), filepath.Join(root, "bots"))
	if err == nil {
		t.Fatal("expected an error when fixtures/inputs.json is missing from the given dir")
	}
}

// A declared input port that nothing reads is a promise the bot doesn't
// keep: a swarm can snap real data into it, the planner will type-check
// that snap, and the value is then silently discarded. The AI composer sees
// these ports in the catalog too, so it can wire one in good faith.
//
// Seven of them existed when this test was written — two voice_sample
// ports, past_posts, kb, two template ports and a schedule. Four were
// wired up; three couldn't be honoured at all and were removed.
func TestNoBotDeclaresAnInputNothingReads(t *testing.T) {
	root := repoRoot(t)
	botsDir := filepath.Join(root, "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatal(err)
	}

	var dead []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(botsDir, e.Name())
		nb, err := schema.LoadNanobot(filepath.Join(dir, "nanobot.yaml"))
		if err != nil {
			continue
		}

		// Everything a step could read the port through: a template
		// reference anywhere in the spec, or {{name}} in a prompt file.
		raw, err := os.ReadFile(filepath.Join(dir, "nanobot.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		haystack := string(raw)
		for _, s := range nb.Spec.Steps {
			if s.PromptFile == "" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, filepath.Clean(s.PromptFile)))
			if err == nil {
				haystack += string(body)
			}
		}

		for _, p := range nb.Spec.Ports.Inputs {
			if strings.Contains(haystack, "inputs."+p.Name) ||
				strings.Contains(haystack, "{{"+p.Name+"}}") {
				continue
			}
			dead = append(dead, e.Name()+"."+p.Name)
		}
	}

	if len(dead) > 0 {
		t.Errorf("these input ports are declared but nothing reads them, so a snap into one is silently discarded:\n  %s\n"+
			"Either wire the port into a step or a prompt, or remove it from the port list.",
			strings.Join(dead, "\n  "))
	}
}

// Some prompt variables label themselves: an optional file input, the
// user's own instructions, a recall answer. Each arrives as a delimited
// block that names itself, or as nothing at all when there's no value.
//
// So a prompt must not introduce one with a header of its own. It reads
// fine while the value is present and breaks silently when it isn't: the
// header is left pointing at whatever follows it, which in this catalog is
// the {{instructions}} block — telling the model the user's own words are
// the writing sample, or the support knowledge base. Four prompts did
// exactly that (content-ideas, draft-replies, post-writer, support-triage);
// nothing failed, the output was just quietly worse.
//
// A *required* file input is different — it is not wrapped, always has a
// value, and needs the prompt to say what it is. Hence the check keys off
// step.SelfDescribingPromptVars rather than off every variable.
func TestNoPromptLabelsAVariableThatLabelsItself(t *testing.T) {
	root := repoRoot(t)
	botsDir := filepath.Join(root, "bots")

	var offenders []string
	for _, id := range allBotIDs(t, botsDir+"/..") {
		dir := filepath.Join(botsDir, id)
		nb, err := schema.LoadNanobot(filepath.Join(dir, "nanobot.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		for _, s := range nb.Spec.Steps {
			if s.PromptFile == "" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, filepath.Clean(s.PromptFile)))
			if err != nil {
				t.Fatalf("%s: read %s: %v", id, s.PromptFile, err)
			}
			lines := strings.Split(string(body), "\n")
			for _, name := range step.SelfDescribingPromptVars(nb, s) {
				if line, header, ok := headerAbove(lines, name); ok {
					offenders = append(offenders, fmt.Sprintf(
						"%s/%s:%d: {{%s}} is introduced by %q, but it labels itself",
						id, s.PromptFile, line, name, header))
				}
			}
		}
	}

	if len(offenders) > 0 {
		t.Errorf("these prompts introduce a self-labelling variable with a header of their own,"+
			" which is left dangling whenever the value is absent:\n  %s\n"+
			"Delete the header line — the block says what it is.",
			strings.Join(offenders, "\n  "))
	}
}

// headerAbove finds {{name}} alone on a line and reports the nearest
// non-blank line above it if that line ends in a colon — the shape of an
// introduction.
func headerAbove(lines []string, name string) (lineNo int, header string, found bool) {
	want := "{{" + name + "}}"
	for i, l := range lines {
		if strings.TrimSpace(l) != want {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			prev := strings.TrimSpace(lines[j])
			if prev == "" {
				continue
			}
			if strings.HasSuffix(prev, ":") {
				return i + 1, prev, true
			}
			break
		}
	}
	return 0, "", false
}

// A memory.recall step needs a question to ask, and needs to survive the
// backend it will actually meet. The default backend is key/value and
// cannot answer questions at all, so a recall step that is neither
// `optional: true` nor backed by a demo fixture fails on a fresh install —
// which is how it shipped for exactly one commit.
//
// Both halves are checked here rather than in the interpreter because the
// interpreter can only complain at runtime, on the user's machine, after
// the run has already started doing work.
func TestEveryMemoryRecallStepIsUsableAsShipped(t *testing.T) {
	root := repoRoot(t)
	botsDir := filepath.Join(root, "bots")

	var problems []string
	seen := 0
	for _, id := range allBotIDs(t, root) {
		dir := filepath.Join(botsDir, id)
		nb, err := schema.LoadNanobot(filepath.Join(dir, "nanobot.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		_, hasFixture := os.Stat(filepath.Join(dir, "fixtures", "memory.recall.json"))
		for _, s := range nb.Spec.Steps {
			if s.Type != "memory.recall" {
				continue
			}
			seen++
			if strings.TrimSpace(s.Query) == "" && strings.TrimSpace(fmt.Sprint(s.Value)) == "" {
				problems = append(problems, fmt.Sprintf(
					"%s.%s has no `query` — there is no question to ask", id, s.Name))
			}
			if !s.Optional && hasFixture != nil {
				problems = append(problems, fmt.Sprintf(
					"%s.%s is required but ships no fixtures/memory.recall.json — it fails"+
						" on the default key/value backend. Mark it `optional: true` or add the fixture.",
					id, s.Name))
			}
		}
	}

	if seen == 0 {
		t.Fatal("no memory.recall steps in the catalog — this test is no longer checking anything")
	}
	if len(problems) > 0 {
		t.Errorf("unusable memory.recall steps:\n  %s", strings.Join(problems, "\n  "))
	}
}

// guardrails.network_egress is enforced now (see step.EgressPolicy), and
// the rule for a bot that declares nothing is that nothing is enforced — a
// promise nobody made is not one to break, and third-party bots should not
// break the day enforcement lands.
//
// That is a loophole this catalog must not use. A bot with a web.fetch step
// takes an arbitrary URL from its own inputs and goes to it; if it declares
// no allowlist it has quietly opted out of the only guardrail that applies
// to it.
func TestABotThatFetchesDeclaresWhereItMayGo(t *testing.T) {
	root := repoRoot(t)
	checked := 0
	for _, id := range allBotIDs(t, root) {
		nb, err := schema.LoadNanobot(filepath.Join(root, "bots", id, "nanobot.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		fetches := false
		for _, s := range nb.Spec.Steps {
			if s.Type == "web.fetch" {
				fetches = true
			}
		}
		if !fetches {
			continue
		}
		checked++
		if len(nb.Spec.Guardrails.NetworkEgress) == 0 {
			t.Errorf("%s has a web.fetch step and declares no guardrails.network_egress —"+
				" say where it may go, or \"*\" if the URLs genuinely come from the user", id)
		}
	}
	if checked == 0 {
		t.Fatal("no bot fetches, so this test is checking nothing")
	}
}

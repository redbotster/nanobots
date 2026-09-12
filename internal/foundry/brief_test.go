package foundry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// repoRootForTest resolves the real repo root, since BuildBrief reads real
// docs/bot-contract.md, .cursor/rules/bot-design.mdc, and reference bots —
// there's no fixture-repo substitute for these, they're this repo's own
// living documentation.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // internal/foundry
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	if _, err := os.Stat(filepath.Join(root, "docs", "bot-contract.md")); err != nil {
		t.Skipf("repo docs not found at %s: %v", root, err)
	}
	return root
}

func TestBuildBriefIncludesEverythingRequired(t *testing.T) {
	root := repoRootForTest(t)
	brief, err := BuildBrief(root, BriefInput{
		Request:           "text me when the dishwasher finishes",
		MissingCapability: "monitor a smart appliance and notify on completion",
		SuggestedInputs:   []schema.InputPort{{Name: "device_id", Type: "string", Required: true}},
		SuggestedOutputs:  []schema.OutputPort{{Name: "notified", Type: "boolean"}},
		ExistingBotIDs:    []string{"content-ideas@0.1.0", "receipt-filer@0.1.0"},
	}, "/usr/local/bin/nanobots")
	if err != nil {
		t.Fatalf("BuildBrief: %v", err)
	}

	for _, want := range []string{
		"only job is to create bots/<some-new-id>/",
		"monitor a smart appliance and notify on completion",
		"text me when the dishwasher finishes",
		"device_id",
		"=== docs/bot-contract.md ===",
		"=== .cursor/rules/bot-design.mdc ===",
		"--- content-ideas/nanobot.yaml ---",
		"--- receipt-filer/nanobot.yaml ---",
		"content-ideas@0.1.0",
		"/usr/local/bin/nanobots conform bots/<id>",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

func TestBuildBriefWorksWithNoSuggestedPortsOrExistingIDs(t *testing.T) {
	root := repoRootForTest(t)
	brief, err := BuildBrief(root, BriefInput{
		Request:           "x",
		MissingCapability: "y",
	}, "/bin/nanobots")
	if err != nil {
		t.Fatalf("BuildBrief: %v", err)
	}
	if !strings.Contains(brief, "y") {
		t.Errorf("brief missing the missing_capability text")
	}
}

// The brief is assembled from the repo at runtime precisely so it cannot go
// stale — and then it carried a hand-written sentence about the repo that
// did. It claimed bots with an ai.generate step "almost always use harness:
// openclaw", which was true before the llm harness existed and is now
// backwards; both worked examples embedded a few lines above it are llm, so
// an agent got a rule and two counterexamples, and a pointer at a 1.1 GB
// Chromium image for a bot needing no browser.
func TestTheBriefsHarnessAdviceMatchesTheCatalog(t *testing.T) {
	root := repoRootForTest(t)
	guidance := harnessGuidance(root)

	if !strings.Contains(guidance, "llm") || !strings.Contains(strings.ToLower(guidance), "ai.generate") {
		t.Errorf("the guidance never points an ai.generate bot at the llm harness:\n%s", guidance)
	}
	// The specific reversal that was there.
	if regexp.MustCompile(`(?i)ai\.generate[^.\n]*openclaw`).MatchString(guidance) {
		t.Errorf("the guidance still sends ai.generate bots to openclaw:\n%s", guidance)
	}
	// openclaw must still be named, with the one reason to use it.
	if !strings.Contains(guidance, "transform.render") {
		t.Errorf("the guidance does not say when openclaw IS right:\n%s", guidance)
	}

	// The counts are read from the catalog, so they have to be the real
	// ones — a number in a prompt is a claim like any other.
	llm, openclaw := 0, 0
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		nb, err := schema.LoadNanobot(filepath.Join(root, "bots", e.Name(), "nanobot.yaml"))
		if err != nil {
			continue
		}
		switch nb.Spec.Harness.Type {
		case "llm":
			llm++
		case "openclaw":
			openclaw++
		}
	}
	if !strings.Contains(guidance, fmt.Sprintf("%d llm", llm)) {
		t.Errorf("guidance does not report the real llm count (%d):\n%s", llm, guidance)
	}
	if !strings.Contains(guidance, fmt.Sprintf("%d openclaw", openclaw)) {
		t.Errorf("guidance does not report the real openclaw count (%d):\n%s", openclaw, guidance)
	}
}

// A worked example the brief embeds is a template an agent copies. One that
// contradicts the brief's own advice teaches the wrong thing twice.
func TestTheWorkedExamplesStillExistAndAgreeWithTheAdvice(t *testing.T) {
	root := repoRootForTest(t)
	for _, id := range referenceBots {
		nb, err := schema.LoadNanobot(filepath.Join(root, "bots", id, "nanobot.yaml"))
		if err != nil {
			t.Fatalf("worked example %q is gone: %v", id, err)
		}
		if nb.Spec.Harness.Type == "openclaw" {
			t.Errorf("%s is openclaw, and the brief tells agents to avoid it — pick an example that agrees", id)
		}
	}
}

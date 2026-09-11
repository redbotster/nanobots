package foundry

import (
	"os"
	"path/filepath"
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

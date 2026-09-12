package foundry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// referenceBots are the worked examples embedded verbatim in every brief —
// together they cover every fixture convention a new bot is likely to need:
// content-ideas is llm + one ai.generate step with a typed
// list<json> output (a schemas/*.json reference); receipt-filer is a
// service.call against a connection: demo service plus ai.generate, showing
// the fixtures/<service>.<op>.json convention.
var referenceBots = []string{"content-ideas", "receipt-filer"}

// BuildBrief assembles the one string handed to a coding agent to author
// exactly one new bot. It's read fresh from disk at job-start (not
// go:embed), matching how internal/runner.EnsureHarnessImage already
// resolves harness/*/Dockerfile at runtime rather than baking it into the
// binary.
func BuildBrief(repoRoot string, in BriefInput, binPath string) (string, error) {
	var b strings.Builder

	b.WriteString("You are authoring exactly one new nanobot for the Nanobots catalog. Your only job is to create bots/<some-new-id>/ — nothing else.\n\n")
	b.WriteString("HARD BOUNDARY, repeated because it matters: you must never read, create, or edit any file outside bots/<that one new id>/. Anything you touch outside it is discarded automatically after you finish — there is no exception to this.\n\n")

	fmt.Fprintf(&b, "The capability the existing catalog is missing: %s\n", in.MissingCapability)
	if in.Request != "" {
		fmt.Fprintf(&b, "The original request that surfaced this gap, for color: %q\n", in.Request)
	}
	if len(in.SuggestedInputs) > 0 || len(in.SuggestedOutputs) > 0 {
		b.WriteString("A starting hint at the ports this bot might need (not a mandate — use your own judgment):\n")
		for _, p := range in.SuggestedInputs {
			fmt.Fprintf(&b, "  input:  %s (%s)\n", p.Name, p.Type)
		}
		for _, p := range in.SuggestedOutputs {
			fmt.Fprintf(&b, "  output: %s (%s)\n", p.Name, p.Type)
		}
	}
	b.WriteString("\n")

	contract, err := os.ReadFile(filepath.Join(repoRoot, "docs", "bot-contract.md"))
	if err != nil {
		return "", fmt.Errorf("read docs/bot-contract.md: %w", err)
	}
	b.WriteString("=== docs/bot-contract.md ===\n")
	b.Write(contract)
	b.WriteString("\n\n")

	rules, err := os.ReadFile(filepath.Join(repoRoot, ".cursor", "rules", "bot-design.mdc"))
	if err != nil {
		return "", fmt.Errorf("read .cursor/rules/bot-design.mdc: %w", err)
	}
	b.WriteString("=== .cursor/rules/bot-design.mdc ===\n")
	b.Write(rules)
	b.WriteString("\n\n")

	b.WriteString("Two complete worked examples, every file, verbatim — together they cover every fixture convention you're likely to need:\n\n")
	for _, id := range referenceBots {
		if err := writeBotDirVerbatim(&b, filepath.Join(repoRoot, "bots", id)); err != nil {
			return "", err
		}
	}

	b.WriteString(harnessGuidance(repoRoot))

	if len(in.ExistingBotIDs) > 0 {
		ids := append([]string(nil), in.ExistingBotIDs...)
		sort.Strings(ids)
		fmt.Fprintf(&b, "Existing catalog ids (collision-avoidance only — never edit these): %s\n\n", strings.Join(ids, ", "))
	}

	b.WriteString("Procedure:\n")
	b.WriteString("1. Choose a short kebab-case id that isn't in the list above.\n")
	b.WriteString("2. Write nanobot.yaml, bot.md, prompts/*.md as needed, fixtures/inputs.json (required), fixtures/ai.generate.json if you have an ai.generate step, fixtures/<service>.<op>.json per service.call step, fixtures/memory.recall.json if you have a memory.recall step that isn't `optional: true`, and schemas/*.json for typed json/list<json> outputs.\n")
	b.WriteString("   A list<json> output without a schemas/*.json is a real cost, not a style point: a swarm can only fan out over its fields (chaser.drafted.*.subject) and the AI composer can only see fields it is shown, so an unschema'd list is a port nothing downstream can reach into.\n")
	fmt.Fprintf(&b, "3. To check your work, run exactly: %s conform bots/<id> — this is the only shell command you should ever need. Keep fixing and re-running it until it reports the bot conforms, then stop.\n", binPath)

	return b.String(), nil
}

func writeBotDirVerbatim(b *strings.Builder, botDir string) error {
	return filepath.WalkDir(botDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(botDir), path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "--- %s ---\n", filepath.ToSlash(rel))
		b.Write(content)
		if len(content) == 0 || content[len(content)-1] != '\n' {
			b.WriteString("\n")
		}
		b.WriteString("\n")
		return nil
	})
}

// harnessGuidance states the harness convention from what the catalog
// actually does, rather than from a sentence someone wrote once.
//
// That sentence said "bots with an ai.generate step almost always use
// harness: openclaw", which was true before the llm harness existed and is
// now backwards — 18 bots are llm and 4 are openclaw. Worse, both worked
// examples embedded a few lines above it are llm, so the brief handed an
// agent a rule and two counterexamples, and pointed it at a 1.1 GB Chromium
// image for a bot that needs no browser.
//
// Counting is not just a fix for that instance. This brief is assembled
// from the repo at runtime precisely so it cannot go stale, and a
// hand-written claim about the repo defeats that.
func harnessGuidance(repoRoot string) string {
	counts := map[string]int{}
	entries, err := os.ReadDir(filepath.Join(repoRoot, "bots"))
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			nb, err := schema.LoadNanobot(filepath.Join(repoRoot, "bots", e.Name(), "nanobot.yaml"))
			if err != nil {
				continue
			}
			counts[nb.Spec.Harness.Type]++
		}
	}

	var b strings.Builder
	b.WriteString("Picking a harness — three values, and the catalog's own split right now")
	if total := counts["bare"] + counts["llm"] + counts["openclaw"]; total > 0 {
		fmt.Fprintf(&b, " (%d bare, %d llm, %d openclaw of %d)", counts["bare"], counts["llm"], counts["openclaw"], total)
	}
	b.WriteString(":\n")
	b.WriteString("- bare — fixed steps, no LLM, no browser.\n")
	b.WriteString("- llm — fixed steps that call ai.generate. THIS IS THE ONE for almost any bot with an ai.generate step. Same 25 MB image as bare, because the model call is a callback to nanobotd rather than anything inside the container.\n")
	b.WriteString("- openclaw — only when a step does transform.render to pdf or png. This image is 1.1 GB, almost all Chromium; do not reach for it otherwise.\n")
	b.WriteString("claude-code, opencode, openclaude and hermes name dynamic agent loops this build does not implement, and are rejected by name.\n\n")
	return b.String()
}

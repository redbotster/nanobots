package runner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

// The whole point of the change, measured against the real catalog rather
// than asserted: the bots that need a 1Claw agent should need far fewer
// agents than there are of them.
//
// This account sat at 27 agents against a plan cap of 50, every one of them
// created by this repo, one per bot name — which is what makes
// `Agent limit reached` a thing that happens mid-run rather than a thing
// you read about.
func TestTheCatalogNeedsAHandfulOfAgentsNotOnePerBot(t *testing.T) {
	botsDir := filepath.Join(repoRootForTest(t), "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string][]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nb, err := schema.LoadNanobot(filepath.Join(botsDir, e.Name(), "nanobot.yaml"))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		if !needsOneClawAgent(nb) {
			continue
		}
		byName[agentNameFor(nb)] = append(byName[agentNameFor(nb)], e.Name())
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Logf("%-24s %d bots: %s", name, len(byName[name]), strings.Join(byName[name], ", "))
	}

	bots := 0
	for _, b := range byName {
		bots += len(b)
	}
	// Loose on purpose. Pinning the exact number would make adding a bot
	// with a new guardrail profile look like a regression, which it is not.
	// What must stay true is the shape: agents are a property of policy, not
	// of catalog size.
	if len(byName) >= bots {
		t.Errorf("%d agents for %d bots — agents are still being minted per bot", len(byName), bots)
	}
	if len(byName) > 5 {
		t.Errorf("%d distinct guardrail profiles across the catalog; that is more policies than "+
			"this catalog means to have, and each one is a plan slot", len(byName))
	}
}

// Two bots that declare the same guardrails are the same thing to 1Claw and
// share an agent. Two that differ must not, because shroud_config is set per
// agent and the chat request carries no per-call override — so a collision
// would silently run one bot under another's policy.
func TestAgentNamesFollowGuardrailsNotBotNames(t *testing.T) {
	bot := func(name, pii string, injection, budget float64, provider string) *schema.Nanobot {
		nb := &schema.Nanobot{}
		nb.Metadata.Name = name
		nb.Spec.Guardrails.PII = pii
		nb.Spec.Guardrails.InjectionThreshold = injection
		nb.Spec.Guardrails.DailyBudgetUSD = budget
		nb.Spec.Model.Provider = provider
		return nb
	}

	base := bot("inbox-triage", "redact", 0.7, 0, "anthropic")
	same := bot("draft-replies", "redact", 0.7, 0, "anthropic")
	if agentNameFor(base) != agentNameFor(same) {
		t.Errorf("two bots with identical guardrails got different agents: %s vs %s",
			agentNameFor(base), agentNameFor(same))
	}

	for _, tc := range []struct {
		what string
		nb   *schema.Nanobot
	}{
		{"a different PII policy", bot("x", "allow", 0.7, 0, "anthropic")},
		{"a different injection threshold", bot("x", "redact", 0.9, 0, "anthropic")},
		{"a different provider", bot("x", "redact", 0.7, 0, "openai")},
		// The one a name built from the readable half alone would miss.
		{"a different daily budget", bot("x", "redact", 0.7, 25, "anthropic")},
	} {
		if agentNameFor(tc.nb) == agentNameFor(base) {
			t.Errorf("%s shares an agent with the base profile (%s) — it would run under the "+
				"wrong shroud_config", tc.what, agentNameFor(base))
		}
	}

	// Readable half first: an account's agent list should say something.
	if !strings.HasPrefix(agentNameFor(base), "nanobots-redact-") {
		t.Errorf("agent name %q does not lead with what it is", agentNameFor(base))
	}
}

// A name is a stable identity: it is what EnsureAgent looks up, and what the
// saved credential file is called. Changing how it is derived orphans every
// agent an account already has.
func TestAnAgentNameIsStable(t *testing.T) {
	nb := &schema.Nanobot{}
	nb.Metadata.Name = "inbox-triage"
	nb.Spec.Guardrails.PII = "redact"
	nb.Spec.Guardrails.InjectionThreshold = 0.7
	nb.Spec.Model.Provider = "anthropic"

	const want = "nanobots-redact-e7ecfa"
	if got := agentNameFor(nb); got != want {
		t.Errorf("agent name = %q, want %q.\nChanging this orphans every agent already "+
			"created under the old name, and their api_keys are shown once.", got, want)
	}
}

func TestSafeLabel(t *testing.T) {
	for in, want := range map[string]string{
		"redact":                            "redact",
		"Redact!!":                          "redact",
		"":                                  "agent",
		"!!!":                               "agent",
		"averylongpolicynamethatkeepsgoing": "averylongpolicynamet",
	} {
		if got := safeLabel(in); got != want {
			t.Errorf("safeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

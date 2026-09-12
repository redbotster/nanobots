package wiring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redbotster/nanobots/internal/llm"
	"github.com/redbotster/nanobots/internal/oneclaw"
)

func envFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nanobots.env")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func quiet(string, ...any) {}

// Shroud wins whenever 1Claw is configured, even with direct keys sitting
// right there. That is not alphabetical luck: Shroud is the only backend
// that bills against a per-agent budget, redacts PII and secrets before the
// prompt leaves the machine, and screens for injection. Silently preferring
// a direct key would drop all three, and nothing in a run would look
// different.
func TestShroudIsPreferredWheneverOneClawIsConfigured(t *testing.T) {
	path := envFile(t,
		"ONECLAW_API_KEY=oc-key",
		"ANTHROPIC_API_KEY=sk-ant",
		"GEMINI_API_KEY=AIza",
	)
	g, err := BuildLLM(path, oneclaw.NewClient("oc-key"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	if !llm.IsDeferredShroud(g) {
		t.Fatalf("got %T (%s), want Shroud", g, g.Describe())
	}
}

// With no 1Claw, one key is the whole setup step — nothing else to
// configure. This is the case that used to produce a silently
// demo-fixtures-only install.
func TestOneDirectKeyIsEnough(t *testing.T) {
	for _, tc := range []struct{ env, want string }{
		{"ANTHROPIC_API_KEY=sk-ant", "anthropic (direct)"},
		{"OPENAI_API_KEY=sk-oai", "openai (direct)"},
		{"GEMINI_API_KEY=AIza", "gemini (direct)"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			g, err := BuildLLM(envFile(t, tc.env), nil, quiet)
			if err != nil {
				t.Fatal(err)
			}
			if g == nil {
				t.Fatal("no generator built from a perfectly good key")
			}
			if g.Describe() != tc.want {
				t.Errorf("Describe() = %q, want %q", g.Describe(), tc.want)
			}
		})
	}
}

// An explicit choice beats detection — including choosing a direct provider
// while 1Claw is configured, which is a legitimate thing to want (a cheaper
// model for a batch job, say). It costs the guardrails, and the log line
// says so.
func TestAnExplicitChoiceOverridesDetection(t *testing.T) {
	path := envFile(t, "ONECLAW_API_KEY=oc-key", "GEMINI_API_KEY=AIza", "NANOBOTS_LLM=gemini")
	g, err := BuildLLM(path, oneclaw.NewClient("oc-key"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	if llm.IsDeferredShroud(g) {
		t.Error("an explicit NANOBOTS_LLM=gemini was overridden by 1Claw")
	}
	if g.Describe() != "gemini (direct)" {
		t.Errorf("Describe() = %q", g.Describe())
	}
}

// Asking for a backend whose key is missing must fail loudly at startup.
// Falling back to another provider would send prompts somewhere the
// operator didn't choose; falling back to fixtures would make every bot
// quietly produce canned output.
func TestAskingForABackendWithNoKeyIsAnError(t *testing.T) {
	for _, kind := range []string{"anthropic", "openai", "gemini"} {
		t.Run(kind, func(t *testing.T) {
			_, err := BuildLLM(envFile(t, "NANOBOTS_LLM="+kind), nil, quiet)
			if err == nil {
				t.Fatal("expected an error naming the missing key")
			}
			if !strings.Contains(strings.ToUpper(err.Error()), "API_KEY") {
				t.Errorf("err = %v, want the missing env var named", err)
			}
		})
	}
	if _, err := BuildLLM(envFile(t, "NANOBOTS_LLM=shroud"), nil, quiet); err == nil {
		t.Error("NANOBOTS_LLM=shroud with no 1Claw key should fail")
	}
}

// Nothing configured is a legitimate state — the default install, before
// any key exists. It must produce no generator rather than an error, so
// conformance and demo runs still work.
func TestNoKeysAtAllMeansNoGeneratorNotAnError(t *testing.T) {
	g, err := BuildLLM(envFile(t, "# nothing here"), nil, quiet)
	if err != nil {
		t.Fatalf("an unconfigured install should not fail to start: %v", err)
	}
	if g != nil {
		t.Errorf("got %T, want nil", g)
	}
	// And explicitly asking for none is honoured even with keys present.
	g, err = BuildLLM(envFile(t, "GEMINI_API_KEY=AIza", "NANOBOTS_LLM=none"), nil, quiet)
	if err != nil || g != nil {
		t.Errorf("NANOBOTS_LLM=none: got %v, %v", g, err)
	}
}

// The openai backend is the one that serves every chat-completions gateway
// — OpenRouter, Groq, vLLM, Ollama. A custom base URL has to survive
// wiring, and has to be visible afterwards, or Settings claims prompts are
// going to OpenAI while they go to a box in the next room.
func TestAnOpenAICompatibleEndpointIsCarriedThroughAndNamed(t *testing.T) {
	path := envFile(t,
		"OPENAI_API_KEY=ollama",
		"OPENAI_BASE_URL=http://localhost:11434/v1",
		"NANOBOTS_LLM_MODEL=llama3.1",
	)
	g, err := BuildLLM(path, nil, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.Describe(), "localhost:11434") {
		t.Errorf("Describe() = %q, want the real endpoint", g.Describe())
	}
	o, ok := g.(*llm.OpenAI)
	if !ok {
		t.Fatalf("got %T", g)
	}
	if o.Model != "llama3.1" {
		t.Errorf("model = %q — NANOBOTS_LLM_MODEL did not survive wiring", o.Model)
	}
}

// A typo must not silently become "no LLM", which would look like a working
// install producing demo output forever.
func TestAnUnknownBackendNamesTheRealOnes(t *testing.T) {
	_, err := BuildLLM(envFile(t, "NANOBOTS_LLM=claude"), nil, quiet)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"shroud", "anthropic", "openai", "gemini", "none"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, does not mention %q", err, want)
		}
	}
}

// Shroud is per-agent, so at startup it is a marker. It still needs
// something behind it for anything that generates outside a bot run, and
// that something is whatever direct key is also configured.
func TestShroudCarriesADirectFallbackWhenOneIsAvailable(t *testing.T) {
	path := envFile(t, "ONECLAW_API_KEY=oc", "GEMINI_API_KEY=AIza")
	g, err := BuildLLM(path, oneclaw.NewClient("oc"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := g.(*llm.DeferredShroud)
	if !ok {
		t.Fatalf("got %T", g)
	}
	if d.Fallback == nil {
		t.Fatal("no fallback, so anything generating before an agent exists has nothing to use")
	}
	if d.Fallback.Describe() != "gemini (direct)" {
		t.Errorf("fallback = %q", d.Fallback.Describe())
	}
}

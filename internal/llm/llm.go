// Package llm is where an ai.generate step's prompt actually goes.
//
// It exists because that used to be one line — `l.Shroud.Chat(...)` — which
// meant live text generation required 1Claw and nothing else could provide
// it. Someone holding a Gemini key and no 1Claw account had no live LLM at
// all: every bot in the catalog fell back to demo fixtures, and nothing
// said why.
//
// So this mirrors internal/memory: one small interface, several backends,
// picked once at startup and reported in /api/status. The backends are
//
//	Shroud     1Claw's proxy — token billing, per-agent budgets, PII
//	           redaction and injection screening applied to every call.
//	           The default whenever 1Claw is configured, because those
//	           protections are the reason this project uses 1Claw at all.
//	Anthropic  api.anthropic.com directly.
//	OpenAI     api.openai.com, and anything speaking its chat-completions
//	           shape — OpenRouter, Together, Groq, vLLM, LiteLLM, Ollama.
//	           One backend, many providers, via BaseURL.
//	Gemini     generativelanguage.googleapis.com directly.
//
// Shroud is deliberately not just "another provider". It is the only one
// that can enforce a budget or redact a secret before the prompt leaves the
// machine, so it stays the default and the others are the fallback, not the
// other way round.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/redbotster/nanobots/internal/schema"
)

// Generator turns one prompt into text, for one bot's declared model.
type Generator interface {
	Generate(ctx context.Context, prompt string, model schema.Model) (string, error)
	// Describe names this backend for logs, /api/status and Settings.
	Describe() string
}

// ErrNoGenerator is what an ai.generate step meets when the deployment has
// no LLM at all — neither 1Claw nor a provider key. Named so callers can
// tell it from a provider outage and say something useful.
var ErrNoGenerator = errors.New("no LLM is configured — set ONECLAW_API_KEY for 1Claw token billing, " +
	"or one of ANTHROPIC_API_KEY / OPENAI_API_KEY / GEMINI_API_KEY (see docs/llm.md)")

// defaultMaxTokens is used when a bot declares no limit. Every provider
// here either requires the field or behaves unhelpfully without it —
// Anthropic rejects the request outright.
const defaultMaxTokens = 4096

func maxTokensFor(m schema.Model) int {
	if m.MaxTokens > 0 {
		return m.MaxTokens
	}
	return defaultMaxTokens
}

// resolveModel picks the model name to send.
//
// A bot declares what it wants — `provider: anthropic, name:
// claude-sonnet-4-6` — but the deployment decides what is actually
// reachable. When those agree, the bot's own choice is honoured. When they
// don't, sending `claude-sonnet-4-6` to Gemini is a guaranteed 404, so the
// backend substitutes the model it can actually serve and says so.
//
// Substituting rather than failing is the right call for this catalog: the
// bots declare a sensible default, not a hard requirement, and a user with
// one key should be able to run all of them. Saying so out loud is what
// keeps that from being a silent downgrade.
func resolveModel(serves, fallback string, m schema.Model) (name string, substituted bool) {
	if strings.EqualFold(m.Provider, serves) && m.Name != "" {
		return m.Name, false
	}
	return fallback, true
}

// substitutionNote is the one place the "not the model you asked for"
// wording lives, so every backend reports it identically.
func substitutionNote(asked schema.Model, serves, using string) string {
	if asked.Name == "" {
		return ""
	}
	return fmt.Sprintf("this bot asks for %s/%s; this deployment serves %s, using %s",
		asked.Provider, asked.Name, serves, using)
}

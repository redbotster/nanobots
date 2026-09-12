# Which model a bot actually talks to

Every `ai.generate` step sends its prompt somewhere. This is that choice.

## Backends

| `NANOBOTS_LLM` | Needs | Guardrails |
|---|---|---|
| `shroud` *(default when 1Claw is configured)* | `ONECLAW_API_KEY` | per-agent token budget, PII + secret redaction, injection screening |
| `anthropic` | `ANTHROPIC_API_KEY` | none |
| `openai` | `OPENAI_API_KEY`, optional `OPENAI_BASE_URL` | none |
| `gemini` | `GEMINI_API_KEY` | none |
| `none` | — | bots return their demo fixtures |

Leave `NANOBOTS_LLM` unset and it works out what this machine has: 1Claw if configured, otherwise the first direct key present, otherwise nothing. **Adding one key to `~/.secrets/nanobots.env` is the whole setup step.**

`NANOBOTS_LLM_MODEL` overrides the model used when a bot asks for a provider the backend can't serve (see below).

## Shroud is the default on purpose

It is the only backend that does anything other than forward the prompt. It bills tokens against the agent's daily budget, redacts PII and secrets before the prompt leaves the machine, and screens for prompt injection — all configured per bot from that bot's own `guardrails:` block (see `runner.agentRequestFor`). Those protections are why this project uses 1Claw at all, so a direct provider key is a fallback, not an equal option, and Settings says plainly when you're running without them.

You can still choose one deliberately — `NANOBOTS_LLM=anthropic` with 1Claw configured is honoured. The log line at startup states what you gave up.

## One backend, many providers

The `openai` backend is not one vendor: `/chat/completions` is a de facto wire format, and OpenRouter, Together, Groq, Fireworks, DeepInfra, vLLM, LiteLLM and Ollama all speak it. Point `OPENAI_BASE_URL` at any of them.

```
OPENAI_API_KEY=ollama
OPENAI_BASE_URL=http://localhost:11434/v1
NANOBOTS_LLM_MODEL=llama3.1
```

Settings then reads `openai-compatible (http://localhost:11434/v1)`, not "openai" — naming the real endpoint matters when the answer to "where are my prompts going" is "a box in the next room".

**Shroud is deliberately not served by that backend**, despite exposing `/v1/chat/completions`. It requires an `X-Shroud-Provider` header that no chat-completions client sends, and authenticates with an agent id and key rather than a bare bearer token. That's Shroud doing its job — it has to know which provider a call is billed and screened against — so it gets its own backend rather than being bent into a shape that would lose that.

## When the bot's model isn't available

Every bot declares one, e.g. `provider: anthropic, name: claude-sonnet-4-6`. If your backend serves that provider, the bot gets exactly what it asked for. If it doesn't, sending the name anyway is a guaranteed 404 — so the backend substitutes the model it can serve and logs the swap into the run:

```
[watch/ai.generate] this bot asks for anthropic/claude-sonnet-4-6; this deployment serves gemini, using gemini-3.5-flash-lite
```

Substituting rather than failing is deliberate: the bots' declared models are a sensible default, not a hard requirement, and one key should run the whole catalog. Saying so out loud is what keeps that from being a silent downgrade.

Shroud never substitutes. Each agent is created with `AllowedProviders` set to its own bot's declared provider, so the bot's model is by construction the one that agent may call. Asking for another returns `403 provider not allowed by agent policy` — the guardrail working, not an error to route around.

## Transient failures

Providers 429 and 503 as a matter of course. Those are retried up to three times, honouring `Retry-After` when it's under a minute and backing off 1s/2s otherwise; 4xx is not retried, because that's the provider saying "not ever". The first real Gemini call this package ever made returned `503 model is overloaded` and took down a swarm that had already done real work — the identical call succeeded seconds later. That's what the retry is for.

A `Retry-After` longer than a minute means a quota window rather than a blip, and fails the step instead of holding a container open to wait it out.

## Verified

- **Shroud**, end to end: `competitor-watch` in a real container, `ai.generate -> ok`, against a live 1Claw agent provisioned on demand.
- **Gemini**, end to end, same bot and same swarm, changed only by `NANOBOTS_LLM=gemini`: the substitution notice appeared in the run log, and the run succeeded.
- **Key redaction**, confirmed by a real failure rather than only a test. Gemini authenticates in the query string, and the error carries the URL — a live 503 surfaced as `...:generateContent?key=REDACTED returned 503`, with Google's own message intact.
- **Every request shape**, against an httptest server asserting each provider's own documented format: `/v1/messages` with `x-api-key` + `anthropic-version` for Anthropic, `/chat/completions` with a bearer for OpenAI, `models/{m}:generateContent?key=` with `generationConfig.maxOutputTokens` for Gemini.

## The gap worth knowing about

A direct key alone is now enough to run bots live. It wasn't: `ai.generate` called Shroud directly, so a machine with an `ANTHROPIC_API_KEY` and no 1Claw account ran the entire catalog against demo fixtures — every generation returning canned text, with nothing in the log saying the key was being ignored. `BuildDeps` now treats "has an LLM" and "has 1Claw" as separate facts. 1Claw is still what adds real service calls, vault credentials and Shroud's guardrails on top.

**Honcho cannot currently route through Shroud.** Honcho's config lets you override a base URL and an API-key env var, but not add a header — and Shroud requires `X-Shroud-Provider`. So a local Honcho's own LLM spend goes direct to its provider, outside 1Claw's budget. Closing that needs a small header-injecting proxy in front of Shroud; it isn't built.

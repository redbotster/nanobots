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

## What you give up with a direct key

Worth being concrete, because it is invisible in a run: 1Claw Shroud bills
tokens against a per-agent daily budget, redacts PII and secrets before the
prompt leaves the machine, and screens for prompt injection. A direct
provider key does none of that — the prompt goes straight to the model.

Every bot declares `pii: redact` and an `injection_threshold` in its
guardrails, and those are Shroud's to apply. The bot Inspector used to show
them ticked under a heading reading "enforced by 1Claw" no matter which
backend was configured, which was true for exactly one of four. It now says
"declared by this bot" and names where the prompts are actually going.

Approvals and `max_runtime_secs` are different: this runner enforces those
itself, so they hold whatever model backend you use, and the panel still
claims them.

## Anything else that talks to a model

Two things in this repo generate text without being a bot, and both go through the same layer:

- **The composer** (`POST /api/compose`) — the "describe what you want automated" box. It used to build its own Shroud client and demand `ONECLAW_API_KEY` specifically, so someone with a Gemini key had a working catalog, working bots, and a compose box that returned 400 from the product's primary entry point. It now uses whatever backend is configured, resolving Shroud against its own agent.
- **Honcho**, the recall-capable memory backend — see below.

## Routing Honcho through Shroud

```sh
HONCHO_LLM=shroud ./docker/honcho/honcho.sh up -d
```

Worth doing, because the text Honcho handles is the most sensitive in the system: what's in someone's inbox, what they promised whom, what they escalate. By default that goes straight from a local container to a model provider with nothing in between. Through Shroud it gets billed against a daily budget, redacted for PII and secrets, and screened for injection first.

This works because Shroud speaks `/v1/chat/completions` and forwards tools faithfully — confirmed live, `tool_choice: "auto"` included, with OpenAI-shaped `tool_calls` coming back, which Honcho's dialectic depends on. What Shroud also needs is an `X-Shroud-Provider` header and an agent `id:key` pair, and Honcho can only be given a base URL and an API-key env var. So nanobotd exposes a shim at `/shroud/v1` that adds exactly those two things and forwards the body byte for byte.

The shim is the one route on nanobotd that authenticates. Everything else it serves is inert if reached; this one spends money, and it's reachable from more places than the rest of the API — Docker containers get to the host through `host.docker.internal` even though nanobotd binds loopback, which is precisely how Honcho reaches it. The token is generated on first start, stored `0600` at `~/.nanobots/state/agents/shroud-proxy-token`, and stable across restarts so a running Honcho doesn't break when nanobotd restarts. Spend is billed to its own `nanobots-shroud-proxy` agent with its own daily ceiling, rather than quietly eating a bot's budget.

**Two honest limits.** Embeddings still go direct to Gemini: Shroud's embeddings route wants a provider key stored in the 1Claw vault, and an agent is scoped to one provider anyway — so the embedding model still sees observation text. And the model must be one the shim's agent may call; that agent is created with `AllowedProviders: [anthropic]`, so `config.shroud.toml` uses an Anthropic model.

Verified end to end: an observation posted to Honcho was derived through Shroud (`observation_count=3`), the dialectic answered a real question about it, and a full `inbox-autopilot` run recalled 1100 and 1820 characters through the whole chain — bot → nanobotd → Honcho → shim → Shroud → model.

## What it costs

`nanobots spend` reports what 1Claw has billed this account for model
tokens this period, and the credit balance if there is one.

Account-wide, not per-run. 1Claw bills per agent and this build creates one
agent per bot, so the total is what it can honestly report; attributing a
figure to a single run would mean inventing arithmetic nobody can check.

"Nothing metered yet" is a real answer and distinct from zero — an account
with token billing enabled but no metered activity has no upcoming-invoice
line, and rendering that as $0.00 claims a measurement nobody made. An
account not on token billing at all gets told so, rather than a zero that
reads as free: spend on a direct provider key is between you and that
provider, and nothing here can see it.

## Seeing what it cost

`nanobots spend` prints the figure, and the Model section of Settings shows
the same one. Both read 1Claw's token-billing status, so they cannot
disagree.

Only 1Claw can answer this. On a direct provider key — Gemini, Anthropic,
an OpenAI-shaped gateway — the spend is between you and that provider and
nothing here can see it. That case says "not metered here" rather than
showing $0.00: a confident zero that actually means "no idea" looks like
information, which makes it worse than saying nothing.

"Nothing metered yet" and "$0.00" are also kept apart, for the same reason.

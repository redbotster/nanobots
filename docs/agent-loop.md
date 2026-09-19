# `agent.loop`, and what it needed that wasn't here

Every step type before this one is a fixed, pre-written list: `spec.steps`
says exactly what happens, in exactly what order, and the interpreter just
runs it. `agent.loop` is the one place a bot's own behavior is decided at
run time — the model sees a goal and a set of tools, picks one, sees the
result, and decides the next one, until it has an answer or hits its cap.

## The policy

```yaml
- name: research
  type: agent.loop
  goal: "Has anything changed on {{inputs.url}} since last time? Summarise it."
  tools:
    - name: fetch_page
      builtin: web.fetch
    - name: recall_last_summary
      service: memory_store
      op: get
    - name: send_summary
      service: slack
      op: chat.postMessage
      writes: "post a message to Slack"
  max_iterations: 6
  output: summary
```

- **`goal`** is templated exactly like every other step's fields — the same
  `{{inputs.*}}`/`{{steps.*}}` context, resolved once before the first
  iteration.
- **`tools`** is the complete, explicit list of what the model may call —
  written by the bot's own author, not derived from `spec.services`. A
  tool is either `service: <id>` + `op:` (dispatched through the exact same
  `deps.ServiceCall` a fixed `service.call` step uses) or
  `builtin: web.fetch | memory.get | memory.put` (the three primitives that
  don't need a declared service). Naming both, or neither, is a plan-time
  error.
- **`max_iterations`** is required, 1–20 — the same bound `loop:` uses, and
  the same reasoning: a model deciding "one more tool call" against its own
  judgement is not a thing this runs unattended. Hitting it fails the step
  loudly, with the transcript of every call attached, rather than quietly
  returning whatever partial answer existed at the time.
- **`writes`** on a tool marks it as a real external effect — a one-line
  summary of what calling it does, not a boolean. Any tool with `writes`
  set is held behind `deps.Approve` before it runs, the identical inline
  gate `bots/email-send-approved` already uses before its own send. This is
  reused rather than invented: a write chosen by a model mid-loop is held to
  the same standard as a write from a fixed step list, through the same
  mechanism, not a second one.

## What actually runs a tool call

Every tool call goes through exactly the `step.Deps` a fixed step of the
same shape would use:

| tool | dispatches to |
|---|---|
| `service: X, op: Y` | `deps.ServiceCall` — `connection: demo` answers from a fixture here exactly as it does for an ordinary `service.call` step |
| `builtin: web.fetch` | `deps.WebFetch` |
| `builtin: memory.get` / `memory.put` | `deps.MemoryGet` / `deps.MemoryPut` |

Nothing new was built for any of these — the whole point of routing an
agentic loop through the same primitives a fixed step list already has is
that the safety properties those primitives carry (fixture serving in demo
mode, the container never holding a real credential, the approval gate)
apply for free, rather than needing a second, parallel implementation that
could disagree with the first.

**What isn't wired up yet:** `transform.*`. Those are pure computation over
a value already sitting in a step's own `ctx` — picking a field, building a
literal — and a tool call's result needs to go straight back to the model
as its own message, which is a different enough shape that it's left for
when a real bot actually needs it rather than built speculatively.

## Tool-calling needed a real LLM, and this build didn't have one

Every backend before this — `internal/llm.Generator` — is one prompt in,
one completion out. That's enough for `ai.generate`, and it is not enough
for a model to decide which of several tools to call, see the result, and
decide the next one. `internal/llm.ToolCaller` is the new, optional
interface a backend advertises by implementing it — the identical pattern
`memory.Recaller` already uses on `memory.Store` — and `agent.loop`'s own
error when the configured backend can't do it
(`llm.ErrNoToolCalling`) names the two that can:

**Shroud** — verified against production with a real funded key
(`docs/1claw-feature-requests.md` #13): `tools` + `tool_choice: "auto"` on
`shroud.1claw.co/v1/chat/completions` returns real `tool_calls`, a
`role: "tool"` reply round-trips back to a final answer, and streaming
forwards `tool_calls` delta chunks correctly. Shroud forwards the request
body unmodified, so this is exactly OpenAI's own tool-calling shape,
un-changed.

**Anthropic (direct)** — the Messages API's own shape, which is
meaningfully different from OpenAI's: content is an array of typed blocks
(`text`, `tool_use`, `tool_result`) rather than a flat string plus a `tool`
role, and a tool result goes back as a `user` turn carrying a
`tool_result` block — Anthropic has no `tool` role at all.
`internal/llm/anthropic.go` does its own translation rather than sharing
Shroud's, because the two shapes are different enough that forcing one
through the other's converter would be the wrong kind of code reuse.

**`internal/llm.Gemini` and the generic OpenAI-compatible backend do not
implement `ToolCaller` yet — as *direct* backends.** This is narrower than
"agent.loop can't use Gemini": routing `model.provider: gemini` *through
Shroud* already works, verified live on this account, a `nanobots` Shroud
agent whose policy allows `gemini` and not `anthropic`:

```
turn 1: {"role":"user","content":"What's the weather in Paris? Use the get_weather tool."}
  -> Done=false ToolCalls=[{Name:get_weather Arguments:{"city":"Paris"}}]
turn 2: + the tool's own result -> Done=true
  Content="The weather in Paris is sunny with a temperature of 18 degrees Celsius."
```

Shroud does the provider translation either way, so this needed no new
code — `llm.Shroud.GenerateWithTools` doesn't know or care which provider
answered. What's actually missing is the *direct* path: a deployment with
only a `GEMINI_API_KEY` and no 1Claw configured has no tool-calling
backend at all, since `internal/llm.Gemini` (straight to
`generativelanguage.googleapis.com`) hasn't been built or verified against
its own native function-calling shape. Same for the generic
OpenAI-compatible backend — it could very plausibly speak this exact shape
already, but hasn't been checked against a live endpoint, and this repo's
own standard (`docs/team.md`, `docs/1claw-feature-requests.md`) is to
verify against a real invocation before claiming a capability. `agent.loop`
on either direct backend fails at run time with `llm.ErrNoToolCalling`,
naming the two direct backends that do work, rather than silently falling
back to something else.

## What a fixture looks like

`nanobots conform` needs a deterministic transcript, since a real model
would answer differently every run. `fixtures/agent_loop.json` is a JSON
array, one entry per call `deps.GenerateWithTools` makes, consumed in
order:

```json
[
  {"tool_calls": [{"id": "call_1", "name": "fetch_page", "arguments": "{\"url\":\"https://example.com\"}"}]},
  {"content": "Nothing has changed since last time.", "done": true}
]
```

Each entry mirrors `llm.ToolCallResult` directly — either `tool_calls` (the
model calling something) or `content` + `done: true` (a final answer) —
so what's on disk reads the same as the transcript above, not a dump of a
Go struct. Calling `GenerateWithTools` more times than the fixture has
entries is a fixture that doesn't cover what the bot actually does, and
fails loudly rather than looping back to the start or returning nothing.

**"A real run can become test data" applies here too.**
`internal/step.RecordingDeps.GenerateWithTools` records each turn as the
bot actually made it, in order, onto exactly this file — the same
mechanism every other step type's fixtures already come from.

## Verified live, not just in `go test`

Every unit test above uses a scripted fake. The transcript in the previous
section is the real proof: a real `nanobots` Shroud agent on this account,
a real tool call, a real result fed back, a real final answer that used
it. Two independent live verifications now exist for
`docs/1claw-feature-requests.md` #13 — the user's own production check
used Anthropic; this one used Gemini through the same Shroud endpoint, on
a different account, and got the identical shape of result.

## What's deliberately not built yet

- **No catalog bot uses `agent.loop`.** The mechanism itself is proven live
  end to end (see above), but that was a hand-written probe against a
  synthetic tool, not a real bot doing real work. Converting a real bot (a
  natural candidate: `competitor-watch`, which already fetches a page and
  reasons about what changed — exactly the shape a loop suits) is the next
  real step, and it should happen with a live run against a funded Shroud
  key, not merely with a passing fixture.
- **No spend ceiling.** The original design for this feature wanted a
  `max_spend_usd` alongside `max_iterations`. It isn't here:
  `llm.ToolCallResult` carries no per-call token count or cost from any
  backend, so a field like that would be declared in YAML and checked
  nowhere — worse than not having it. `guardrails.daily_budget_usd` is the
  real ceiling today, enforced by Shroud itself per agent.
- **`transform.*` tools.** See above.
- **Gemini and the generic OpenAI-compatible backend.** See above.

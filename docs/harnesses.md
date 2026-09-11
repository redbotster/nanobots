# Harnesses

`spec.harness.type` in a `nanobot.yaml` names the agent loop that's supposed to drive a bot: `claude-code`, `opencode`, `openclaude`, `hermes`, `openclaw`, or `bare` (no LLM loop — deterministic steps only). The blueprint's vision is that this loop decides, dynamically, how to use tools and services to produce a bot's declared outputs.

**This build doesn't implement that yet.** Both `bare` and `openclaw` run the exact same universal step interpreter (`internal/step.Interpret`) against a bot's declared `spec.steps`, in the order they're written. The only real difference between the two harness images (`harness/bare`, `harness/openclaw`) is:

- `bare` is distroless with no browser at all — `transform.render`'s `to: pdf` (real HTML→PDF via headless Chrome) isn't available, so `bare` bots either don't render, or render to plain HTML/other non-PDF output.
- `openclaw` adds headless Chromium for that PDF rendering and, later, actual browser-driven steps.

`ai.generate` works identically on both — it's a plain HTTPS callback to nanobotd's Shroud bridge (`internal/step.RemoteDeps.AIGenerate`), not a local model or anything Chromium-shaped, so it needs nothing `bare`'s distroless image doesn't already have. `bots/competitor-watch` is `bare` and uses `ai.generate` for exactly this reason — the catalog's own "bare + ai.generate" note for that brick is correct, and this doc previously said otherwise.

Neither one reasons about *how* to accomplish a step — the steps are already fully specified in YAML, and the interpreter just runs them. That's honest for what this build's two example bots need (their steps are fully deterministic already), but it's not the dynamic agent loop the harness names imply, and it's not pretended to be one anywhere in the code.

`hermes`, `openclaude`, `opencode`, and `claude-code` aren't implemented as harness images at all — `internal/runner.EnsureHarnessImage` returns a clear error naming the harness if a bot declares one of them, rather than silently falling back to something else.

## What a real implementation would change

A real dynamic harness would replace the fixed step-by-step execution in `internal/step.Interpret` with an actual agent loop that has the same primitives (`service.call`, `ai.generate`, `memory.*`, `approve`, `notify`, `transform.render`) available as callable tools, and decides which to use and in what order from a bot's `bot.md` instructions rather than a pre-written `spec.steps` list. The bot contract itself (`docs/bot-contract.md`) — inputs in, outputs out, the container never holding a real credential — doesn't need to change for that; only what runs inside the container does.

## Try it

```
nanobots conform bots/recap-emails-to-pdf   # openclaw harness, deterministic steps
nanobots conform bots/email-drive-file      # bare harness, deterministic steps
```

Both pass identically today because both are honest about running the same interpreter.

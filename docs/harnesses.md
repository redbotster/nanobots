# Harnesses

`spec.harness.type` in a `nanobot.yaml` names the agent loop that's supposed to drive a bot: `claude-code`, `opencode`, `openclaude`, `hermes`, `openclaw`, or `bare` (no LLM loop — deterministic steps only). The blueprint's vision is that this loop decides, dynamically, how to use tools and services to produce a bot's declared outputs.

**This build doesn't implement that yet.** Both `bare` and `openclaw` run the exact same universal step interpreter (`internal/step.Interpret`) against a bot's declared `spec.steps`, in the order they're written. The only real difference between the two harness images (`harness/bare`, `harness/openclaw`) is:

- `bare` is distroless with no browser at all — `transform.render`'s `to: pdf`/`to: png` (real HTML→PDF or HTML→screenshot via headless Chrome) isn't available, so `bare` bots either don't render, or render to plain HTML/other non-image output.
- `openclaw` adds headless Chromium for that PDF/PNG rendering and, later, actual browser-driven steps.

`ai.generate` and `web.fetch` both work identically on either harness — each is a plain HTTPS callback to nanobotd (`internal/step.RemoteDeps.AIGenerate`/`.WebFetch`), not a local model, browser, or anything Chromium-shaped, so neither needs anything `bare`'s distroless image doesn't already have. `bots/competitor-watch` is `bare` and uses both `web.fetch` and `ai.generate` for exactly this reason — the catalog's own "bare + ai.generate" note for that brick is correct, and this doc previously said otherwise.

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


## Which image a bot actually gets

The harness type in a `nanobot.yaml` is the bot's declaration of intent. The image it runs in is chosen from what its steps actually do.

The two images are not close in size:

| image | size | contents |
|---|---|---|
| `nanobots/harness-bare:local` | **25 MB** | distroless static + the interpreter |
| `nanobots/harness-openclaw:local` | **1.1 GB** | Debian slim + Chromium (338 MB) and its shared libraries (299 MB) |

Only `transform.render` to `pdf` or `png` needs a browser, and it needs one *in the container* — `step.RemoteDeps.Render` calls `RenderHTMLToPDF` directly rather than calling back to nanobotd. Everything else, `ai.generate` included, is an HTTP callback to the daemon: the container makes a request and nanobotd does the work. A bot that only generates text needs nothing but the interpreter and a CA bundle.

Nineteen bots declare the `openclaw` harness. Four of them render. The other fifteen were each pulling 1.1 GB to make an HTTP request. `runner.imageFor` now resolves this both ways — a `bare` bot that renders is promoted, an `openclaw` bot that doesn't is dropped to `bare` — and logs which it chose and why:

```
ideas | starting (openclaw harness)
ideas | using the bare image: no step here needs a browser
```

Across the catalog that's **25 of 30 bots on the 25 MB image**; only `render-pdf`, `meeting-prep`, `quote-builder`, `recap-emails-to-pdf` and `sheet-reporter` need the big one. `TestMostCatalogBotsDoNotNeedABrowser` asserts the ratio against the real `bots/` tree, so a render step creeping into a lean bot is something a test notices.

The declared harness type is deliberately left alone. It's the honest statement of what a bot is, and once a real dynamic agent loop exists `openclaw` will mean more than "has Chromium" — this only decides which image to hand it today.

**A latent bug this surfaced**: the promotion check only looked for `to: pdf`, missing `png`. `sheet-reporter` renders a chart that way and happened to work because it already declared `openclaw`; a `bare` bot rendering a png would have been handed an image with no browser in it.

**Still on the table**: the openclaw image itself is unoptimised — Chromium ships ~50 locale packs and a software-GL fallback that headless PDF rendering may not need. Trimming those is a smaller and riskier win than not pulling the image at all, so it hasn't been done.

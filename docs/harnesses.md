# Harnesses

`spec.harness.type` in a `nanobot.yaml` names the agent loop that's supposed to drive a bot: `claude-code`, `opencode`, `openclaude`, `hermes`, `openclaw`, or `bare` (no LLM loop — deterministic steps only). The blueprint's vision is that this loop decides, dynamically, how to use tools and services to produce a bot's declared outputs.

**This build doesn't implement that yet.** Both `bare` and `openclaw` run the exact same universal step interpreter (`internal/step.Interpret`) against a bot's declared `spec.steps`, in the order they're written. The only real difference between the two harness images (`harness/bare`, `harness/openclaw`) is:

- The `bare` *image* is distroless with no browser at all, so inside it `transform.render`'s `to: pdf`/`to: png` (real HTML→PDF or HTML→screenshot via headless Chrome) isn't available. A bot that *declares* `bare` and renders is not stuck with that, though: `imageFor` promotes it to the openclaw image at run time, which is exactly what `bots/render-pdf` relies on. See "Which image a bot actually gets" below.
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

Across the catalog that's **35 of 39 bots on the 25 MB image**; only `render-pdf`, `meeting-prep`, `quote-builder`, `recap-emails-to-pdf` and `sheet-reporter` need the big one. `TestMostCatalogBotsDoNotNeedABrowser` asserts the ratio against the real `bots/` tree, so a render step creeping into a lean bot is something a test notices.

The declared harness type is deliberately left alone. It's the honest statement of what a bot is, and once a real dynamic agent loop exists `openclaw` will mean more than "has Chromium" — this only decides which image to hand it today.

**A latent bug this surfaced**: the promotion check only looked for `to: pdf`, missing `png`. `sheet-reporter` renders a chart that way and happened to work because it already declared `openclaw`; a `bare` bot rendering a png would have been handed an image with no browser in it.

**Still on the table**: the openclaw image itself is unoptimised — Chromium ships ~50 locale packs and a software-GL fallback that headless PDF rendering may not need. Trimming those is a smaller and riskier win than not pulling the image at all, so it hasn't been done.


## The `llm` harness

There are three implemented values, and they describe two independent things that the blueprint's vocabulary conflates: *does this bot use a model*, and *what does its container need*.

| value | means | image |
|---|---|---|
| `bare` | fixed steps, no LLM, no browser | 25 MB |
| `llm` | fixed steps that call an LLM | 25 MB — **the same image** |
| `openclaw` | needs a real browser | 1.1 GB |

`llm` and `bare` share an image on purpose. `ai.generate` is an HTTP callback to nanobotd: the container never talks to a model, so a bot that generates text needs nothing beyond the interpreter and a CA bundle. The value exists to describe the bot honestly, not to add anything to its runtime.

It was added because neither existing value fit the fifteen bots that were declaring `openclaw`. They don't render, so `openclaw` was wrong — but `bare` is documented as "no LLM loop — deterministic steps only", which for a bot whose whole job is `ai.generate` would have been a worse description, not a better one. There was no honest thing to call them.

The catalog now reads: **11 `bare`, 15 `llm`, 4 `openclaw`**.

The visible effect is that a run log stopped correcting itself. Before:

```
ideas | starting (openclaw harness)
ideas | using the bare image: no step here needs a browser
```

After:

```
ideas | starting (llm harness)
```

The correction line still exists and still fires when a declaration and its steps genuinely disagree — `bots/render-pdf` declares `bare` and renders, so it's promoted on every run. Need always wins over declaration; the declaration is what the bot *is*, the steps are what it *needs*.

`claude-code`, `opencode`, `openclaude` and `hermes` remain unimplemented and are still rejected by name. They describe *dynamic agent loops*, which is a different axis again — none of the three implemented harnesses runs one. The only place a real coding agent runs in this build is the foundry (`docs/foundry.md`), which authors new bots rather than running them.


## Images rebuild when the interpreter changes

The agent binary is compiled into the harness image, so an image built before an edit to `internal/step` or `cmd/nanobot-agent` silently runs the old interpreter. This used to be a documented trap — *"a harness image is only as fresh as the last time it was built"* — with `NANOBOTS_REBUILD_HARNESS=1` as the manual escape hatch.

Relying on people remembering that failed in practice. A 36-hour-old image made a prompt-safety change look verified when the container was running code that predated it, and a genuine bug elsewhere was misdiagnosed twice before the stale image was noticed.

`EnsureHarnessImage` now hashes everything that reaches the binary — `cmd/nanobot-agent`, all of `internal`, `go.mod` and `go.sum` — into a `nanobots.agent-source` label at build time, and compares it before reusing an image. A mismatch rebuilds. `_test.go` files are excluded, since they never reach the binary and including them would rebuild the image on every test edit.

`NANOBOTS_REBUILD_HARNESS=1` still forces a rebuild. If the source tree can't be hashed, the old "the image exists, that's good enough" rule applies rather than rebuilding on every run.

## The one thing openclaw can still reach

`guardrails.network_egress` is enforced for `web.fetch` — the step that
takes an arbitrary URL from a bot's inputs — because that step runs on the
host, where the bot that asked and the URL it asked for are both known (see
`docs/bot-contract.md` and `internal/step.EgressPolicy`).

An `openclaw` bot is the exception, and it is worth being exact about why.
`transform.render` runs a real Chromium *inside* the container, so remote
assets referenced by the HTML it renders are fetched by the browser, past
any check this build performs. Nothing in the catalog renders remote assets
— the templates are self-contained — but the guardrail does not stop one
that did.

Closing it needs a per-run Docker network with an egress proxy in front of
it. Until then this is the honest boundary: enforced for the step that
fetches on your behalf, not for a browser you asked to draw a page.

## Running without a container

Most bots do not open one. `runsInProcess` in `internal/runner` decides per
bot, and the run log says which path each took:

```
triage    starting (llm harness, in-process, no container)
meetings  starting (openclaw harness, container: it renders with a real headless browser)
```

**Why that is safe.** A bot's steps are not user code. They are a fixed list
declared in its `nanobot.yaml`, executed by this repo's own interpreter, and
every step that reaches the outside world — `service.call`, `ai.generate`,
`web.fetch`, `memory.*`, `approve`, `notify` — already runs inside
`nanobotd` and is reached from the container by an HTTP callback over a
per-run token. So for those bots the container holds no credential, runs
nothing we did not write, and isolates a process whose only privileged act
is to phone home.

**What still gets one.** Two cases, and both are the isolation argument
actually applying:

- a bot that renders with headless Chromium, which is a browser executing
  pages nobody here wrote. Five of the catalog's thirty-nine.
- a future `harness: agent` loop that decides its own actions at runtime.
  The moment behaviour stops being a declared list, the sandbox matters
  again.

**One honest difference.** `docker kill` ends a hung bot outright and the
in-process path cannot: `step.Interpret` takes no context, so a
`max_runtime_secs` timeout stops the run *waiting* without stopping the
work. Each blocking call carries its own deadline — the HTTP clients, the
LLM client, the approval gate — so the ceiling is a backstop rather than the
only bound. The same property cuts the other way and in our favour: a bot
parked on an approval in-process holds a goroutine, not a container, and
unanswered approvals were the largest consumer of container time on the
machine this was built on.

**Measured.** `morning-brief` went from a median of 35.3s across 14
container runs to 21-27s, and that swarm still puts two of its four bots in
containers, so most of what remains is model latency rather than startup.
The headline is not the seconds: `github-digest-to-slack` was run to success
with `docker` removed from the daemon's PATH entirely.

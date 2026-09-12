# Nanobots

*Legos for AI. Snap micro-agents together, run them anywhere, keep the keys in 1Claw.*

Nanobots is a local-first system for composing single-job AI/deterministic containers ("nanobots") into typed, DAG-shaped workflows ("nanoswarms"), with secrets, OAuth, LLM routing, and guardrails delegated to [1Claw](https://docs.1claw.co).

The full product spec lives in [`context/NANOBOTS-BLUEPRINT.md`](context/NANOBOTS-BLUEPRINT.md) and [`context/NANOBOTS-CATALOG.md`](context/NANOBOTS-CATALOG.md). Treat those as the source of truth for the YAML schemas and the launch catalog; this README covers what's actually built, and is kept in sync with it — if something here contradicts the code, the code wins. `docs/` has one page per concept — contract, connections, harnesses, approvals, the 1Claw bridge, Browser Bridge, the foundry, the scheduler, run history, fan-out, memory, models, parallelism, error policy, supervisors, fixtures — each ending in how to run it for real.

## Why nanobots, not one big agent

The obvious way to automate "recap my inbox, prep my meetings, and post my content" is one big agent with a giant prompt and every tool bolted on. That's also the way you end up with something slow, expensive to run, impossible to debug when it does the wrong thing, and terrifying to grant real Gmail/Stripe/LinkedIn access to, because there's no boundary around what it might decide to do with any of them at once.

Nanobots' bet is the opposite one — the Unix philosophy applied to AI automation: lots of small bricks that each do one job extremely well, wired together instead of merged together. Concretely, that means every bot in `bots/` is built to be:

- **Small** — one job per bot, statable in one sentence. `inbox-triage` sorts mail; it doesn't also draft replies (that's `draft-replies`) or send them (`email-send-approved`).
- **Fast** — most bots run in the `bare` harness (no LLM loop, no browser, a plain deterministic container) and finish in seconds; only the ones that genuinely need Chrome or an LLM call reach for more.
- **Powerful** — small doesn't mean thin. Each bot is backed by a real integration wherever one exists (direct Gmail/Slack/GitHub/Stripe/HubSpot/X/LinkedIn clients, not just fixtures) and does its one job completely, not partially.
- **Modular** — every input and output is a typed, named port (`docs/bot-contract.md`), so a bot never has to know or trust anything about its neighbors beyond the shape of the data crossing the wire.
- **Orchestratable** — because the ports are typed and the contract is uniform, any bot can be snapped into any swarm the planner can type-check, and swapped for another bot with compatible ports without touching anything else. Composing bricks this way, instead of writing one monolithic agent, is what makes 30 bots and 14 swarms possible to build, test, and trust independently — you never have to reason about the whole system to trust one piece of it, and a piece you don't trust yet (a bot still on `connection: demo`) can't leak scope into the pieces you do.

That's the actual product: not a chatbot that does automation, but a growing, composable catalog of small, real, individually-provable automation bricks — plus, since assembling bricks by hand is still work, an AI composer that does the assembly for you from one sentence of plain English (next section).

## Who this is for

The primary customer is a **busy solo operator** — a solo founder, indie hacker, or anyone running their own show without an assistant: inbox triage, meeting prep, content, and follow-ups eat their day, and they'd rather describe the outcome they want than configure automation software. That's who the AI composer, the basic-mode UI, and the swarm gallery are built to hook in one sentence ("Help me automate a daily email recap and list it by priority") — no YAML, no drag-and-drop tutorial, no OAuth screens, a working swarm on the canvas in seconds.

The full catalog also covers a second, secondary persona: a small business owner running light sales/ops (leads, quotes, invoices, reviews) who's ready to graduate from the basic mode into the bot library and the manual builder. Those bricks are real and fully wired, they're just not the first thing a new user sees — see [Basic vs. advanced mode](#basic-vs-advanced-mode).

## Status

This repo implements the full 28-brick, 12-swarm launch catalog from `context/NANOBOTS-CATALOG.md` (Tranches A, B, and C), plus 2 bots and 2 swarms that predate the catalog work — **30 nanobots, 14 nanoswarms** — end to end and verified live, not just type-checked:

- **The bot contract, planner, and Docker-backed local runner**: bots run as real, non-root, read-only-filesystem containers, wired to each other's outputs across separate containers, streamed live over SSE. `nanobots conform` proves every bot honors the contract without Docker; `nanobots plan` type-checks every swarm's snaps against real port types.
- **A real [1Claw](https://docs.1claw.co) bridge**: Human API key → bearer token exchange, agent creation/deletion, Shroud (LLM proxy) chat completions, vault secrets, agent memory, approval requests, and a Browser Bridge client — all exercised against the real API, not mocked.
- **Seven real, standalone direct-service clients** for what 1Claw doesn't natively cover: Google (`internal/google` — Gmail, Drive, Sheets, Calendar), Slack (`internal/slack`), GitHub (`internal/github`), Stripe (`internal/stripe`), HubSpot (`internal/hubspot`), X (`internal/x`), LinkedIn (`internal/linkedin`) — each dispatched from `internal/step.LiveDeps` once a bot's service is switched off `connection: demo` **and** a human has connected the account — plus a credential-free `web.fetch` step for reading public pages. X and LinkedIn share a small, provider-agnostic OAuth2+PKCE core (`internal/oauth2pkce`) rather than each hand-rolling Google's original loopback-redirect flow. None of these is the default connection for any bot out of the box (see [What's real vs. simulated](#whats-real-vs-simulated)).
- **An AI composer** ("the head nanobot" — see below) that turns a plain-English request into a validated draft swarm.
- **A dead-simple WebUI** (Vite + React + TypeScript + Tailwind + Radix) with a basic/advanced mode toggle, a bot library (searchable, grouped by service, with a per-service demo/live switch that connects an account inline), a Fleet view of the bots you've told how to work and the teams they work in, a swarm gallery showing at a glance how much of each swarm is live vs. demo, a live run viewer with SSE log streaming and inline approvals, browser notifications when something needs your approval, a Settings page where connecting a service — including 1Claw itself — is a button or a pasted token, and a visual swarm builder (including picking a nested field of a `json` output, not just whole-port connections) for anyone who wants to build or tweak by hand.
- **Per-bot customisation**: every LLM bot takes an optional `instructions` port with a suggestion written for its job — "Anything mentioning data loss or billing is top priority" — editable from its card in the UI, or overridable per swarm. It shapes how a bot works, never what it's allowed to do; the precedence rule lives in one place in Go rather than in nineteen prompt files (`docs/bot-contract.md`).
- **Any model provider, 1Claw first**: `ai.generate` and the composer both go through one small interface with four backends — 1Claw Shroud (the default, and the only one with a budget ceiling, PII redaction and injection screening), Anthropic, Gemini, and OpenAI-or-anything-speaking-its-format (OpenRouter, Groq, vLLM, Ollama). One key is the whole setup step. A bot asking for a model the backend can't serve gets a substitute, logged into the run rather than swapped silently (`docs/llm.md`).
- **A swarm's independent branches run at once**: the planner always knew the DAG; the runner used to walk it one bot at a time. Six of the fifteen catalog swarms have a wave wider than one — `morning-brief` is four bots in two waves of two (`docs/parallelism.md`). The other nine are straight chains and gain nothing, which is worth saying rather than implying a general speed-up.
- **A real run can become a bot's test data**: fixtures are what `nanobots conform` replays offline, and they're hand-written — a guess at what a model returns, which drifts. A succeeded run offers back exactly what it got, marked new or replacing, side by side with what's committed (`docs/fixtures.md`). Approvals are never recorded, because a pinned "approved" would turn a gate into a rubber stamp.
- **A failure at the edge doesn't lose the run**: a bot can be marked `on_error: continue`, and everything downstream of it is skipped rather than run against missing inputs (`docs/error-policy.md`). `get-paid` used to send every reminder and *then* fail the whole run because it couldn't post a Slack summary; now it finishes, and says plainly that one step didn't. Never silent — a tolerated failure is recorded on the run and rendered as a warning with the same one-click fix a real failure gets.
- **A real scheduler**: every catalog swarm's `trigger: {type: cron, ...}` now actually fires — see `docs/scheduler.md`. Previously nothing in this build ever executed one; every run was a human clicking Run.
- **Run history that survives a restart**: every finished run is kept as a JSON file under `~/.nanobots/history/`, capped at 200 — see `docs/run-history.md`. A failed run shows *why* it failed and offers to run the same swarm again. A run killed mid-flight by a restart is restored as failed rather than sitting in the list as "running" forever.

Not built yet: the `kubernetes`/`apple` compile targets, a real dynamic agent loop (see `docs/harnesses.md` — today's harnesses run a fixed, pre-written step list, not an LLM deciding what to do), enforced network-egress guardrails (reported in a bot's declared guardrails, not actually firewalled), a real OAuth integration for Google Business Profile (`review-responder` stays on `connection: demo`, gap called out in `docs/connections.md`), and the hosted multi-tenant control plane.

## The AI composer — the "head nanobot"

The fastest way to build a swarm is to describe what you want:

> Help me automate a daily email recap and list it by priority

Type that into the box at the top of **Swarms** and click **Automate it**. Under the hood (`internal/api/compose.go`, `POST /api/compose`):

1. The real bot catalog (every bot's id, description, and every input/output port with its type) is handed to Shroud along with your message, via a dedicated `nanobots-composer` 1Claw agent.
2. The model's answer — a proposed set of bots and the snaps connecting them — is parsed into the exact same shape the visual builder already saves.
3. That draft is run through the real planner (`planner.PlanSwarm`), the same type-checker a manual save uses. A hallucinated bot id or a mismatched port type comes back as a normal validation error, not a silent bad save.
4. The validated draft opens directly in the visual builder, pre-populated — you see the assembled swarm on the canvas immediately, free to rename, tweak, or delete a node before saving.

**It never saves or runs anything by itself.** Every swarm the composer proposes still goes through the same human-reviews-before-it's-real path as one built by hand — consistent with this whole product's approval-first philosophy: nothing acts without a human seeing it first.

### The foundry — when the catalog genuinely can't do it

The composer writes in the full language, not a subset: it fans a bot out over a list with `.*`, collapses the results back with `join:`, and marks a trailing notification `on_error: continue` — and the catalog it's shown lists the *fields* inside each json port, because a snap can drill into one and a model shown only types will invent a field that isn't there. Asked for "find every overdue invoice, email each customer a reminder once I approve, then post one summary to Slack", it now produces essentially `get-paid`.

If no combination of existing bots can satisfy the request, the composer says so instead of guessing (`{"gap": true, "missing_capability": "..."}`), and the WebUI offers to escalate: a sandboxed coding agent (Claude Code, running inside its own Docker container — see `docs/foundry.md` for why a container and not just CLI permission flags) authors a brand-new bot, self-tests it against the real conformance runner, and opens the same human-approval gate a swarm's `approve` step uses before the bot ever becomes part of the real catalog. Approve it and the composer automatically retries your original request. This needs its own `ANTHROPIC_API_KEY` (see `docs/foundry.md`) — a real, separate prerequisite from `ONECLAW_API_KEY`, since 1Claw's Shroud proxy can't back a multi-turn, tool-using coding session.

## Basic vs. advanced mode

A toggle in the header (labeled **Advanced**) switches the whole UI between two modes, stored per-browser (`web/src/lib/uiMode.ts`):

- **Basic** (the default for a new user): the nav shows Swarms, Runs, Settings. Swarms opens straight into the composer box and a gallery of ready-made swarms — nothing to learn before you can automate something. No Bot Library, no blank-canvas "build manually" entry point.
- **Advanced**: adds **Bot Library** to the nav and a **Build manually** button to Swarms, revealing the drag-and-drop canvas builder — everything basic mode has, plus the tools to build or edit a swarm by hand, bot by bot, port by port.

The toggle only changes which entry points are visible. It never changes how a swarm actually runs — a swarm built by the composer, built by hand, or hand-edited from a composer draft all execute identically.

## Quick start

Requires Go 1.25+, Node 22+, and Docker running (for real bot execution — everything else works without it).

```
go build ./...
go test ./...

# type-check any example swarm and print its run DAG
go run ./cmd/nanobots plan -f examples/swarms/daily-email-recap.yaml

# run it for real (needs Docker running)
go run ./cmd/nanobots run -f examples/swarms/daily-email-recap.yaml

# or drive it from the WebUI instead — visit http://localhost:5173
go run ./cmd/nanobots up &
cd web && npm install && npm run dev
```

`nanobotd` reads your 1Claw Human API key from `$NANOBOTS_ENV_FILE` (default `~/.secrets/nanobots.env`, `ONECLAW_API_KEY=...`) at startup only — it's never written into this repo, logged, or handed to a bot container (see `docs/oneclaw-bridge.md`). Without *any* model configured, every bot runs in demo mode against its fixtures. 1Claw is the best option — it's the only one that bills against a per-agent budget, redacts PII and secrets, and screens for injection — but it is no longer the only one: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` (including any chat-completions gateway, via `OPENAI_BASE_URL`) or `GEMINI_API_KEY` each work on their own, for bots and for the composer alike (`docs/llm.md`).

Once the WebUI is running, in **basic mode** (the default): type what you want automated into the box at the top of Swarms, review the draft that opens, and hit Save — or just pick one of the ready-made swarms in the gallery below it and click **Run**. Flip the header toggle to **Advanced** for the bot library and the manual canvas builder. **Settings** connects a real account — click **Connect** on Google (real OAuth consent screen) or paste a Slack/GitHub/Stripe/HubSpot token directly; every credential lands in a 1Claw vault secret, never on this machine's disk, and connecting an account never changes a bot's behavior by itself — each bot ships on `connection: demo` until you deliberately switch a specific service to a live connection in its `nanobot.yaml`.

## The catalog

**33 bots** (`bots/`) — 27 job bricks plus 6 utility bricks (`approve`, `notify`, `render-pdf`, `drive-save`, `drive-watch`, `form-to-sheet`), all at `0.1.0`:

| Busy-person / solo-founder story (hero path) | SMB ops story (advanced) |
|---|---|
| `inbox-triage`, `draft-replies`, `follow-up-chaser`, `email-send-approved` | `lead-enricher`, `lead-router`, `quote-builder` |
| `meeting-prep`, `calendar-scheduler`, `meeting-notes-filer` | `invoice-chaser`, `receipt-filer`, `sheet-reporter` |
| `content-ideas`, `post-writer`, `post-publisher`, `repurposer`, `newsletter-drafter` | `support-triage`, `review-responder`, `competitor-watch` |
| `recap-emails-to-pdf`, `email-drive-file`, `github-issues-digest` | `review-board`, `reviewer`, `review-synthesis` (a supervisor team — `docs/supervisors.md`) |

**15 swarms** (`examples/swarms/`), each with a header comment documenting any place it simplifies the catalog's own aspirational diagram (usually: a downstream bot acts on the first item where fanning out would multiply container starts — see `docs/fan-out.md`):

| Swarm | What it does |
|---|---|
| `daily-email-recap`, `daily-inbox-recap` | Recap the inbox to a PDF in Drive, notify or email the link (the two original, pre-catalog swarms). |
| `github-digest-to-slack` | Summarise a repo's newest issues and post the digest to Slack. |
| `inbox-autopilot` | Triage the inbox, draft replies to anything urgent, send them all once approved. |
| `morning-brief` | Triage the inbox and prep today's meetings into one brief. |
| `never-drop-a-thread` | Find sent threads that never got a reply, draft and send a nudge for every one. |
| `content-engine` | Brainstorm post ideas, write up the first one, publish it once approved. |
| `repurpose-everything` | Turn a new Drive file into posts across formats, publish once approved. |
| `lead-to-meeting` | Log, enrich, and route a new lead; draft a scheduling reply once approved. |
| `support-desk-lite` | Triage support mail, flag anything urgent to Slack, send every drafted reply once approved. |
| `bookkeeping-assistant` | File today's receipts and produce a spend report with a chart. |
| `get-paid` | Find every overdue invoice, send each reminder once approved, post one summary of what went out. |
| `meeting-to-action` | File a new transcript's notes, flag action items, draft follow-ups. |
| `weekly-client-report` | Build a client's spend report and email them the link once approved. |
| `supervisor-review` | A review board picks reviewers from your role library, each reviews in parallel, one synthesis reconciles them (`docs/supervisors.md`). |

## Architecture

The design principle behind every layer here: a bot container never gets to touch a real credential, and a human never has to understand OAuth, an API key, or a redirect URI to connect a service. Concretely:

```
nanoswarm.yaml  →  planner  →  runner  →  N Docker containers, wired together
                                    │
                                    ├─ each container runs cmd/nanobot-agent,
                                    │  which executes the bot's spec.steps
                                    │  against internal/step's universal
                                    │  interpreter (same interpreter for every
                                    │  harness type — see docs/harnesses.md)
                                    │
                                    └─ any step needing the outside world
                                       (service.call, ai.generate, web.fetch,
                                       memory, approve, notify) is a callback
                                       to nanobotd over a random per-run
                                       token — never a credential inside
                                       the container
                                             │
                                             ▼
                                    internal/step.LiveDeps
                                       ├─ demo fixtures (connection: demo)
                                       ├─ internal/oneclaw (1Claw Human API +
                                       │  Shroud, for ai.generate/memory/
                                       │  approvals/generic execute_intent)
                                       ├─ internal/google (direct Gmail/
                                       │  Drive/Sheets/Calendar, once
                                       │  connection: oauth_native + a
                                       │  connected account)
                                       ├─ internal/x, internal/linkedin
                                       │  (direct OAuth2+PKCE via the shared
                                       │  internal/oauth2pkce core, once
                                       │  connected)
                                       ├─ internal/github, internal/slack,
                                       │  internal/stripe, internal/hubspot
                                       │  (direct REST, once connected)
                                       └─ web.fetch (plain outbound GET +
                                          text extraction — no vault, no
                                          OAuth, nothing to connect)
```

Every branch is reachable from the exact same `nanobot.yaml`, decided per-service by a single `connection:` field. A bot's steps never know or care which branch actually ran.

**The bot contract** (`docs/bot-contract.md`): a bot is any container that reads `/run/inputs.json` (one already-defaulted, already-validated JSON value per declared input port, mounted read-only alongside the bot's own `nanobot.yaml`/`bot.md`/`prompts`/`fixtures` at `/bot`), writes one file per declared output port under `/run/outputs/`, and exits 0 on success. That's the whole interface — `nanobots conform ./bots/<id>` proves a bot honors it without Docker or a network, by running its `spec.steps` in-process against fixture data (`internal/contract`).

**The step interpreter** (`internal/step`): one Go program, baked into every harness image, that executes a bot's declared `spec.steps` in order — `service.call`, `ai.generate`, `transform.render`/`transform.now`/`transform.pick`, `web.fetch`, `memory.get`/`put`, `approve`, `notify`. It runs identically against three interchangeable backends (`step.Deps`):
- `DemoDeps` — fixture data from `bots/<id>/fixtures/*.json`, no network at all. What `nanobots conform` and the WebUI's demo mode use.
- `LiveDeps` — the real 1Claw Human API + Shroud, with a per-service `connection:` deciding whether a given service call actually hits 1Claw, hits a direct client (Google/Slack/GitHub/Stripe/HubSpot), does a plain `web.fetch`, or falls back to a fixture.
- `RemoteDeps` — what actually runs *inside* a container: every method is an HTTP callback to nanobotd, authenticated by a random token issued for that one run and rejected the instant the run ends. The container-side half of "no credential ever touches the container."

**The planner** (`internal/planner`): parses a `nanoswarm.yaml`, resolves each `bots[].use:`/`path:` reference to a real `nanobot.yaml`, type-checks every `snaps[]` connection against the two bots' declared port types (including dotted field paths into a `json`-typed port's own JSON Schema, and numeric indices into a `list<T>` port), and builds/cycle-checks the run DAG. `nanobots plan -f <swarm.yaml>` runs this standalone; the runner runs it before every real execution; the WebUI's visual builder and the AI composer both run it live before a swarm is ever saved or shown.

**The runner** (`internal/runner`): builds the two harness Docker images on first use (`harness/bare` — distroless, no LLM, no browser, also used by the `llm` harness since `ai.generate` is a callback to nanobotd rather than anything in the container; `harness/openclaw` — same interpreter plus headless Chromium for HTML→PDF/PNG rendering), runs each bot in the planner's topological order as `docker run --rm --read-only --user <non-root>`, mounts a content-addressed blob store for `file`-typed ports, and streams every log line to the run's SSE subscribers as it happens — including the exact moment an `approve` step opens a gate (`docs/approvals.md`).
internal/wiring/    Shared startup wiring, so nanobotd and the CLI build it once

**The 1Claw bridge** (`internal/oneclaw`): a real client for 1Claw's Human API — API-key-for-bearer-token exchange, agent creation/update (including `memory_enabled`, `shroud_config`), Shroud chat completions, vault create/ensure, vault secret read/write (`PutSecret`/`GetSecret`, verified against `@1claw/openapi-spec`), agent memory get/put, approval request/wait, and a Browser Bridge client (pairing, credential bindings, gated browser sessions — real and tested, currently used for none of this build's bots specifically, because it's a dead end for Google; see `docs/browser-bridge.md`).

**Direct service clients** (`internal/google`, `internal/x`, `internal/linkedin`, `internal/slack`, `internal/github`, `internal/stripe`, `internal/hubspot`): standalone REST clients for what 1Claw doesn't natively cover, split into two families. Google, X, and LinkedIn are OAuth flows — Google was built first with its own hand-rolled PKCE+loopback client (no client secret, since 1Claw's own OAuth registry has no Gmail scopes and Browser Bridge is a dead end specifically for Google — it drives a real, CDP-automated browser, and Google refuses sign-in outright on any automation-controlled browser instance); X and LinkedIn came later and share a small, provider-agnostic OAuth2+PKCE core instead (`internal/oauth2pkce`) rather than duplicating that flow a second and third time — X is a true public client like Google (PKCE only, no secret), LinkedIn isn't (it requires a client secret on the token exchange even with PKCE, and may issue no refresh token at all, both documented in `internal/linkedin`'s package doc and handled honestly in `internal/step/linkedin_live.go`). Slack, GitHub, Stripe, and HubSpot are simpler: a token that never expires, so it's paste-once-into-a-vault-secret rather than an OAuth dance (`internal/step/vault_token.go` is the shared "fetch a static secret from the vault at most once per process" logic all four share).

## What's real vs. simulated

Being explicit about this matters more here than in most projects, because so much of what Nanobots *is* is "the layer that hides whether something is real" — from a user's perspective a bot's Gmail call should look the same whether it's live or fixture data, which makes it easy to accidentally paper over what's actually happening. So, plainly:

| Real | Simulated / not yet |
|---|---|
| 1Claw Human API, Shroud, vault secrets, agent memory, approval requests | 1Claw's execution-intent bindings (`internal/oneclaw.Execute`) assume a binding already exists on the agent — provisioning one from a `nanobot.yaml` service isn't wired up |
| Docker execution: non-root, read-only fs, real container-to-container I/O wiring | Guardrails' `network_egress` allowlist is reported per bot, not enforced as an actual container network policy |
| The step interpreter, for every harness type, incl. `web.fetch` and PDF/PNG rendering | The *dynamic agent loop* a harness name like `openclaw` implies — every harness today runs the same fixed, pre-written `spec.steps` list, not an LLM deciding what to do (`docs/harnesses.md`) |
| The Google OAuth client (`internal/google`) — real PKCE flow, real REST calls (Gmail, Drive, Sheets, Calendar), unit-tested against fake servers | No bot ships with a non-demo Google connection by default — every bot's Google service is `connection: demo` until a human deliberately flips it (`docs/connections.md`) |
| The Slack/GitHub/Stripe/HubSpot clients — real REST calls, unit-tested against fake servers; `notify`'s Slack delivery is genuinely wired, not a no-op, once connected | Google Business Profile replies (`review-responder`) need a dedicated integration — out of scope for this pass, stays on `connection: demo`, called out in `docs/connections.md` |
| The X and LinkedIn OAuth2+PKCE clients (`internal/x`, `internal/linkedin`) and the shared `internal/oauth2pkce` core — real token exchange/refresh, real posting calls, unit-tested against fake servers; wired end to end into `post-publisher`'s `x`/`linkedin` services and the WebUI's Settings page | No bot ships with a non-demo `x`/`linkedin` connection by default — like Google, `post-publisher` stays on `connection: demo` until a human deliberately connects a real account and flips it (`docs/connections.md`); live end-to-end posting hasn't been exercised against a real X/LinkedIn developer app in this build, only against fake test servers |
| The AI composer — a real Shroud call, a real planner validation pass, a real hydrate-into-the-builder handoff | The composer never auto-saves or auto-runs; a hallucinated bot id or type mismatch surfaces as a normal validation error for the human to see, by design |
| The unified Connect UI (Settings) — real vault writes/reads, real Google OAuth kicked off server-side | Connections are always whole-port-to-port in the visual builder and the composer (no picking a nested field of a `json` output the way a couple of example swarms do by hand); a swarm's trigger/vars/deploy config has no UI yet; canvas layout isn't persisted |
| Approvals — a run genuinely blocks, flips to `awaiting_approval`, and waits for a real decision from the WebUI or CLI | The local run queue's approvals aren't mirrored into 1Claw's own approval system/mobile app — that's a separate, real queue (`docs/approvals.md`) |
| Per-item fan-out *and* the join back — a `.*` snap runs a downstream bot once per list item with one approval covering the batch, and `join: lines\|json\|count\|flatten\|first` collapses the results into one value for a bot that isn't fanned out (`docs/fan-out.md`). `get-paid` now chases every overdue invoice and posts one summary, where it used to chase the first | Two swarms still snap `.0`: `content-engine` and `meeting-to-action` would fan out cleanly but multiply container starts, which is a cost decision rather than a missing primitive |
| Basic/advanced mode toggle — a real, tested UI gate | Purely a UI-visibility gate; it never changes what actually executes |

**A real constraint hit repeatedly while building and testing this, worth knowing about**: 1Claw vaults can require passkey verification before `GetSecret` succeeds, depending on the account's own vault security tier — a 403 `"Passkey verification required to access vault secrets"` from 1Claw itself, not a bug here. It means a connected credential can sit in the vault but be temporarily unreadable until a human unlocks it with their passkey in a browser. It surfaced again during this session's final live-run pass (the connections status check correctly reported every service as disconnected while it was in effect) and correctly errors bots that need it instead of pretending to deliver. Bots and swarms that only need Shroud (`ai.generate`) or `web.fetch` are unaffected — that's most of the busy-person hero path — and account-level agent caps are a separate, real, tier-based constraint (this account is capped at 10 concurrent agents on its current plan; deleting an unused agent frees the slot instantly since agents are recreated on demand by name).

## Repo layout

```
cmd/nanobotd/       Go daemon — REST+SSE API, binds loopback only
cmd/nanobots/       Go CLI — plan, conform, schema, up, run, connect (init/add/save/publish/compile: not yet)
cmd/nanobot-agent/  the container entrypoint every harness image runs
internal/schema/    Nanobot/Nanoswarm Go types, YAML loading, JSON Schema generation
internal/planner/   resolves a swarm's bots, type-checks snaps, builds/cycle-checks the run DAG
internal/step/      the universal step interpreter + Demo/Live/Remote Deps backends, incl. web.fetch and all direct-service dispatch
internal/contract/  the conformance test runner (`nanobots conform`)
internal/oneclaw/   real 1Claw Human API + Shroud + vaults/secrets + memory + agent CRUD + Browser Bridge client
internal/google/    real Gmail/Drive/Sheets/Calendar OAuth + REST client (PKCE, no client secret)
internal/oauth2pkce/ shared, provider-agnostic OAuth2+PKCE core (used by internal/x, internal/linkedin)
internal/x/         real X (Twitter) OAuth2+PKCE client (post a tweet)
internal/linkedin/  real LinkedIn OAuth2+PKCE client (resolve member URN, post a share)
internal/slack/     real Slack Web API client (chat.postMessage)
internal/github/    real GitHub REST client (issues.list)
internal/stripe/    real Stripe REST client (invoices.list)
internal/hubspot/   real HubSpot CRM REST client (contact search/upsert)
internal/foundry/   the composer's escalation path — a sandboxed coding agent authors a new bot on a real gap, self-tests it, and gates it behind human approval
internal/scheduler/ a real cron scheduler — polls examples/swarms/ and fires any swarm whose trigger is due, no dependency
internal/runner/    Docker-backed orchestrator: builds harness images, runs bots, wires I/O
internal/api/       REST+SSE handlers, incl. the visual builder's + Connect UI's + AI composer's + foundry's endpoints and the container callback endpoints
internal/daemon/    wires the above together; shared by cmd/nanobotd and `nanobots up`
harness/            Dockerfiles for the bot runtime images (bare, openclaw) and the foundry's own agent sandbox (foundry-agent)
schemas/            Generated JSON Schema for Nanobot / Nanoswarm
bots/               Individual nanobots (nanobot.yaml + instructions + fixtures) — 30 today
examples/swarms/    Example nanoswarms — 14 today
web/                WebUI — Vite + React + TypeScript + Tailwind + Radix primitives
  src/pages/           LandingPage, BotLibrary, SwarmsPage (composer + gallery + gap/foundry flow), SwarmView, BuilderPage, FoundryJobPage, RunsPage, RunDetail, SettingsPage
  src/components/      shared UI (run log, results, snap trail, YAML drawer, basic/advanced Switch, BotCard, ...)
  src/components/builder/  the visual swarm builder's canvas, grouped palette, and per-node inspector
  src/lib/uiMode.ts    basic/advanced mode hook (localStorage-backed)
docs/               Concept docs, each ending in how to run it for real
```

## Testing

Per the project's own working style, expensive verification is a single consolidated pass at the end rather than after every piece — here's exactly what that pass covers and how to repeat it.

**1. Everything that's cheap and automated:**

```
go build ./... && go vet ./... && go test ./...
```

459 table-driven Go tests across every package (`grep -rho '^func Test[A-Za-z0-9_]*' --include='*_test.go' . | sort -u | wc -l`, so the number stays checkable), including:
- `internal/contract`'s `TestRunConformanceOnLaunchBots` — auto-discovers and conformance-tests all 30 bots under `bots/` against their own fixtures, no Docker or network.
- `internal/planner`'s `TestPlanAllExampleSwarms` — auto-discovers and type-checks all 14 swarms under `examples/swarms/`.
- httptest-mocked 1Claw/Google/Slack/GitHub/Stripe/HubSpot/X/LinkedIn API clients, built against each provider's real, documented endpoint shapes (verified against `@1claw/openapi-spec` and each provider's own docs, not guessed).
- `internal/runner`'s run-history tests — a run really written to a temp dir, a second store really reading it back, plus the awkward cases: a corrupt file, an over-cap directory, and a run left mid-flight by a restart (`docs/run-history.md`).

```
cd web && npx tsc -b && npm run test
```

TypeScript strict-mode compilation and Vitest + Testing Library component tests, including the AI composer's request/response types and the `useUIMode` hook.

**2. Real Docker executions against the live 1Claw API** (needs `ONECLAW_API_KEY`, Docker running):

```
go run ./cmd/nanobots run -f examples/swarms/never-drop-a-thread.yaml
go run ./cmd/nanobots run -f examples/swarms/content-engine.yaml
```

Both were run for real this session, end to end: real openclaw/bare Docker containers, a real Shroud-generated draft, a real approval gate answered from the terminal, and — for `content-engine` specifically — two independent live `ai.generate` calls chained across separate containers followed by a real (demo-connection) publish. Both finished `succeeded`.

**3. A live Puppeteer pass** exercising the two things a screenshot proves better than a unit test: the AI composer producing a real validated draft from the project's own example prompt, and the basic/advanced toggle actually gating the UI. Driven ad hoc against a real `nanobotd` + `vite dev`, not a project dependency — the shape of it:

1. Load the app, log in, confirm basic mode's nav has no "Bot library" entry.
2. Type `Help me automate a daily email recap and list it by priority` into the composer box, click **Automate it**.
3. Poll for the builder to open with a validated draft (a real `POST /api/compose` round trip to Shroud). Confirmed this session: a 4-bot draft (`inbox-triage` → `recap-emails-to-pdf` → `drive-save` → `notify`) came back named "Daily Priority Email Recap", already marked "ready to run" by the planner.
4. Flip the header toggle to Advanced, confirm "Bot library" and "Build manually" now appear.

That practice has also caught real bugs no unit test would have over the course of this build: the visual builder's own null-array crash and snap-trail mislabeling, a CSS Grid layout bug where the bot palette silently couldn't scroll past 11 bots, and a pre-existing `email-drive-file` timeout bug (its `max_runtime_secs: 60` guardrail was too short for its own approval gate to ever be answered in time).

## License

MIT

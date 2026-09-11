# Nanobots

*Legos for AI. Snap micro-agents together, run them anywhere, keep the keys in 1Claw.*

Nanobots is a local-first system for composing single-job AI/deterministic containers ("nanobots") into typed, DAG-shaped workflows ("nanoswarms"), with secrets, OAuth, LLM routing, and guardrails delegated to [1Claw](https://docs.1claw.co).

The full product spec lives in [`context/NANOBOTS-BLUEPRINT.md`](context/NANOBOTS-BLUEPRINT.md) and [`context/NANOBOTS-CATALOG.md`](context/NANOBOTS-CATALOG.md). Treat those as the source of truth for the YAML schemas and the launch catalog; this README covers what's actually built, and is kept in sync with it — if something here contradicts the code, the code wins. `docs/` has one page per concept — contract, connections, harnesses, approvals, the 1Claw bridge, Browser Bridge — each ending in how to run it for real.

## Status

This repo implements **Phase 0 + a working slice of Phase 1/2** of the blueprint's roadmap, end to end and verified live — not just type-checked:

- **12 nanobots** across the catalog's utility and job tranches (`bots/`) — Gmail/Drive/Sheets triage, drafting, sending, watching, and PDF/render bricks; a GitHub issues digest; and the generic `approve`/`notify` utility bricks.
- **5 example nanoswarms** (`examples/swarms/`) wiring those bots together, from the original single-recap-and-email swarm through a multi-bot inbox-autopilot swarm with a human approval gate, to a GitHub-to-Slack digest that touches neither Google nor 1Claw's execution bindings at all.
- A real bot contract, planner (parse → resolve → type-check snaps → build/cycle-check the DAG), and a Docker-backed local runner: bots run as real, non-root, read-only-filesystem containers, wired to each other's outputs across separate containers, streamed live over SSE.
- A real [1Claw](https://docs.1claw.co) bridge: Human API key → bearer token exchange, agent creation/deletion, Shroud (LLM proxy) chat completions, vault secrets, agent memory, approval requests, and a Browser Bridge client — all exercised against the real API, not mocked.
- Three real, standalone direct-service clients for what 1Claw doesn't natively cover — Google (`internal/google`, OAuth), Slack (`internal/slack`, bot token), GitHub (`internal/github`, personal access token) — each dispatched from `internal/step.LiveDeps` once a bot's service is switched off `connection: demo` **and** a human has connected the account. None is the default for any bot yet (see [What's real vs. simulated](#whats-real-vs-simulated)).
- A WebUI (Vite + React + TypeScript + Tailwind + Radix) with a bot library, a swarm gallery, a live run viewer with SSE log streaming and inline approvals, a **Settings page where connecting Google/Slack/GitHub is a button or a pasted token — no CLI required** — and **a visual swarm builder**: drag bots onto a canvas grouped by service, connect typed ports, start from an existing swarm as a template, and save the result as a real, runnable `nanoswarm.yaml`.

Not built yet: the other catalog bricks/swarms beyond what's listed above, the `kubernetes`/`apple` compile targets, a real dynamic agent loop (see `docs/harnesses.md` — today's harnesses run a fixed, pre-written step list, not an LLM deciding what to do), enforced network-egress guardrails (reported in a bot's declared guardrails, not actually firewalled), and the hosted multi-tenant control plane.

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

runs both of that swarm's bots as real Docker containers; gets a real Shroud-generated summary through a real 1Claw agent; renders a real PDF; blocks on a real approval gate (answered from the terminal or the WebUI); wires one bot's output into the next bot's input across two separate containers; and finishes successfully. Every bot's Google service still runs against fixture data rather than a real account by default — see [`docs/connections.md`](docs/connections.md) for exactly how to turn that on for real, and why it isn't on by default (short version: Google blocks sign-in outright on any automation-controlled browser, so the obvious Browser Bridge approach hit a real wall — documented, not hidden).

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
                                       (service.call, ai.generate, memory,
                                       approve, notify) is a callback to
                                       nanobotd over a random per-run token —
                                       never a credential inside the container
                                             │
                                             ▼
                                    internal/step.LiveDeps
                                       ├─ demo fixtures (connection: demo)
                                       ├─ internal/oneclaw (1Claw Human API +
                                       │  Shroud, for ai.generate/memory/
                                       │  approvals/generic execute_intent)
                                       ├─ internal/google (direct Gmail/
                                       │  Drive/Sheets, once connection:
                                       │  oauth_native + a connected account)
                                       ├─ internal/github (direct issues.list,
                                       │  once connected)
                                       └─ internal/slack (direct chat
                                          post, for `notify` steps whose
                                          channel targets "slack:...")
```

Every one of those four branches — demo, 1Claw, Google, GitHub/Slack — is reachable from the exact same `nanobot.yaml`, decided per-service by a single `connection:` field. A bot's steps never know or care which branch actually ran.

**The bot contract** (`docs/bot-contract.md`): a bot is any container that reads `/run/inputs.json` (one already-defaulted, already-validated JSON value per declared input port, mounted read-only alongside the bot's own `nanobot.yaml`/`bot.md`/`prompts`/`fixtures` at `/bot`), writes one file per declared output port under `/run/outputs/`, and exits 0 on success. That's the whole interface — `nanobots conform ./bots/<id>` proves a bot honors it without Docker or a network, by running its `spec.steps` in-process against fixture data (`internal/contract`).

**The step interpreter** (`internal/step`): one Go program, baked into every harness image, that executes a bot's declared `spec.steps` in order — `service.call`, `ai.generate`, `transform.render`/`transform.now`/`transform.pick`, `memory.get`/`put`, `approve`, `notify`. It runs identically against three interchangeable backends (`step.Deps`):
- `DemoDeps` — fixture data from `bots/<id>/fixtures/*.json`, no network at all. What `nanobots conform` and the WebUI's demo mode use.
- `LiveDeps` — the real 1Claw Human API + Shroud, with a per-service `connection:` deciding whether a given service call actually hits 1Claw, hits Google directly (`internal/google`, see below), or falls back to a fixture.
- `RemoteDeps` — what actually runs *inside* a container: every method is an HTTP callback to nanobotd, authenticated by a random token issued for that one run and rejected the instant the run ends. The container-side half of "no credential ever touches the container."

**The planner** (`internal/planner`): parses a `nanoswarm.yaml`, resolves each `bots[].use:`/`path:` reference to a real `nanobot.yaml`, type-checks every `snaps[]` connection against the two bots' declared port types (including dotted field paths into a `json`-typed port's own JSON Schema, and numeric indices into a `list<T>` port), and builds/cycle-checks the run DAG. `nanobots plan -f <swarm.yaml>` runs this standalone; the runner runs it before every real execution; the WebUI's visual builder runs it live, debounced, before a swarm is ever saved.

**The runner** (`internal/runner`): builds the two harness Docker images on first use (`harness/bare` — distroless, no LLM, no browser; `harness/openclaw` — same interpreter plus headless Chromium for HTML→PDF rendering), runs each bot in the planner's topological order as `docker run --rm --read-only --user <non-root>`, mounts a content-addressed blob store for `file`-typed ports, and streams every log line to the run's SSE subscribers as it happens — including the exact moment an `approve` step opens a gate (`docs/approvals.md`).

**The 1Claw bridge** (`internal/oneclaw`): a real client for 1Claw's Human API — API-key-for-bearer-token exchange, agent creation/update (including `memory_enabled`, `shroud_config`), Shroud chat completions, vault create/ensure, vault secret read/write (`PutSecret`/`GetSecret`, verified against `@1claw/openapi-spec`), agent memory get/put, approval request/wait, and a Browser Bridge client (pairing, credential bindings, gated browser sessions — real and tested, currently used for none of this build's Google-touching bots specifically, because Google blocks it; see `docs/browser-bridge.md`).

**Direct Google access** (`internal/google`): since 1Claw's own OAuth provider registry has no Gmail scopes and Browser Bridge is a dead end specifically for Google, this is a small, real, standalone OAuth client — PKCE, no client secret, a loopback redirect server, direct REST calls to Gmail/Drive/Sheets. Connecting it (the WebUI's Settings page, or `nanobots connect google`) runs the one-time interactive consent flow and stores the resulting refresh token in a 1Claw vault secret (never on local disk); `internal/step.LiveDeps` dispatches any service with `provider: google` and a non-`demo` connection to it.

**Direct Slack/GitHub access** (`internal/slack`, `internal/github`): the same idea, simpler, since a Slack bot token or GitHub personal access token never expires — no OAuth dance, just paste it into Settings (or `nanobots connect`) once and it's stored the same way. `internal/step/vault_token.go` is the shared "fetch a static secret from the vault at most once per process" logic both use. Slack is wired specifically into the `notify` step type (a `channel: "slack:#..."` value routes there); GitHub is wired as a normal `service.call` provider like Google.

## What's real vs. simulated

Being explicit about this matters more here than in most projects, because so much of what Nanobots *is* is "the layer that hides whether something is real" — from a user's perspective a bot's Gmail call should look the same whether it's live or fixture data, which makes it easy to accidentally paper over what's actually happening. So, plainly:

| Real | Simulated / not yet |
|---|---|
| 1Claw Human API, Shroud, vault secrets, agent memory, approval requests | 1Claw's execution-intent bindings (`internal/oneclaw.Execute`) assume a binding already exists on the agent — provisioning one from a `nanobot.yaml` service isn't wired up |
| Docker execution: non-root, read-only fs, real container-to-container I/O wiring | Guardrails' `network_egress` allowlist is reported per bot, not enforced as an actual container network policy |
| The step interpreter, for every harness type | The *dynamic agent loop* a harness name like `openclaw` implies — every harness today runs the same fixed, pre-written `spec.steps` list, not an LLM deciding what to do (`docs/harnesses.md`) |
| The Google OAuth client (`internal/google`) — real PKCE flow, real REST calls, unit-tested against fake servers | Live end-to-end testing against a real Google account (blocked on a `GOOGLE_OAUTH_CLIENT_ID`), and no bot ships with `connection: oauth_native` by default — every bot's Google service is `connection: demo` until a human deliberately flips it (`docs/connections.md`) |
| The Slack/GitHub clients (`internal/slack`, `internal/github`) — real REST calls, unit-tested against fake servers; `notify`'s Slack delivery is genuinely wired, not a no-op, once connected | A live end-to-end post/fetch on this project's own development account is currently blocked on a real 1Claw vault security feature — see below |
| The unified Connect UI (Settings) — real vault writes/reads, real Google OAuth kicked off server-side | — |
| The visual swarm builder — real save-to-YAML, real live type-checking against the same planner a run uses, palette grouping/connection badges reflect real per-service state | Connections are always whole-port-to-port (no picking a nested field of a `json` output the way `daily-email-recap.yaml`'s `recap.recap_json.headline` snap does by hand); a bot's manual input values are always plain strings; a swarm's trigger/vars/deploy config has no UI yet; canvas layout isn't persisted |
| Approvals — a run genuinely blocks, flips to `awaiting_approval`, and waits for a real decision from the WebUI or CLI | The local run queue's approvals aren't mirrored into 1Claw's own approval system/mobile app — that's a separate, real queue (`docs/approvals.md`) |

**A real constraint hit while building this, worth knowing about**: 1Claw vaults can require passkey verification before `GetSecret` succeeds, depending on the account's own vault security tier — a 403 `"Passkey verification required to access vault secrets"` from 1Claw itself, not a bug here. It means a connected credential can sit in the vault but be temporarily unreadable until a human unlocks it with their passkey. `github-issues-digest` still ran completely for real up to that point (it only reads demo fixtures by default); it's `notify`'s live Slack delivery specifically that surfaced this, and it correctly errored instead of pretending to deliver.

## Repo layout

```
cmd/nanobotd/       Go daemon — REST+SSE API, binds loopback only
cmd/nanobots/       Go CLI — plan, conform, schema, up, run, connect (init/add/save/publish/compile: not yet)
cmd/nanobot-agent/  the container entrypoint every harness image runs
internal/schema/    Nanobot/Nanoswarm Go types, YAML loading, JSON Schema generation
internal/planner/   resolves a swarm's bots, type-checks snaps, builds/cycle-checks the run DAG
internal/step/      the universal step interpreter + Demo/Live/Remote Deps backends, incl. Google dispatch
internal/contract/  the conformance test runner (`nanobots conform`)
internal/oneclaw/   real 1Claw Human API + Shroud + vaults/secrets + memory + agent CRUD + Browser Bridge client
internal/google/    real Gmail/Drive/Sheets OAuth + REST client (PKCE, no client secret)
internal/slack/     real Slack Web API client (chat.postMessage)
internal/github/    real GitHub REST client (issues.list)
internal/runner/    Docker-backed orchestrator: builds harness images, runs bots, wires I/O
internal/api/       REST+SSE handlers, incl. the visual builder's + Connect UI's endpoints and the container callback endpoints
internal/daemon/    wires the above together; shared by cmd/nanobotd and `nanobots up`
harness/            Dockerfiles for the bot runtime images (bare, openclaw)
schemas/            Generated JSON Schema for Nanobot / Nanoswarm
bots/               Individual nanobots (nanobot.yaml + instructions + fixtures) — 12 today
examples/swarms/    Example nanoswarms — 5 today
web/                WebUI — Vite + React + TypeScript + Tailwind + Radix primitives
  src/pages/           LandingPage, BotLibrary, SwarmsPage, SwarmView, BuilderPage, RunsPage, RunDetail, SettingsPage
  src/components/      shared UI (run log, results, snap trail, YAML drawer, ...)
  src/components/builder/  the visual swarm builder's canvas, grouped palette, and per-node inspector
docs/               Concept docs, each ending in how to run it for real
```

## Quick start

Requires Go 1.25+, Node 22+, and Docker running (for real bot execution — everything else works without it).

```
go build ./...
go test ./...

# type-check the example swarm and print its run DAG
go run ./cmd/nanobots plan -f examples/swarms/daily-email-recap.yaml

# run it for real (needs Docker running)
go run ./cmd/nanobots run -f examples/swarms/daily-email-recap.yaml

# or drive it from the WebUI instead — visit http://localhost:5173
go run ./cmd/nanobots up &
cd web && npm install && npm run dev
```

`nanobotd` reads your 1Claw Human API key from `$NANOBOTS_ENV_FILE` (default `~/.secrets/nanobots.env`, `ONECLAW_API_KEY=...`) at startup only — it's never written into this repo, logged, or handed to a bot container (see `docs/oneclaw-bridge.md`). Without a key, every bot runs fully in demo mode.

Once the WebUI is running:
- **Settings** connects a real account: click **Connect** on Google (opens your browser for a real OAuth consent screen) or paste a Slack bot token / GitHub personal access token directly. Every credential lands in a 1Claw vault secret, never on this machine's disk. (The same three flows exist as CLI-only fallbacks: `GOOGLE_OAUTH_CLIENT_ID=...` in the env file plus `nanobots connect google`; see `docs/connections.md`.)
- Connecting an account doesn't change any bot's behavior by itself — every bot ships on `connection: demo` until you deliberately switch a specific service to a live connection method in its `nanobot.yaml`.
- **Swarms → + New swarm** opens the visual builder: click bots in from the palette (grouped by service, each showing a demo/live/not-connected pill), drag a connection from an output dot to an input dot, fill in any inputs that aren't wired from another bot, or start from an existing swarm as a template. **Save** writes a real `examples/swarms/<name>.yaml`, validated against the same planner a run uses, before it ever touches disk.

## Testing

- `go build ./...`, `go vet ./...`, `go test ./...` — 165+ table-driven Go tests across every package, including httptest-mocked 1Claw/Google/Slack/GitHub API clients (real endpoint shapes, verified against `@1claw/openapi-spec` and each provider's own docs, not guessed) and full conformance tests for every bot.
- `cd web && npx tsc -b && npm run test` — TypeScript strict-mode compilation and Vitest + Testing Library component tests.
- Every WebUI change in this build has also been verified against a real, running `nanobotd` + `vite dev` in an actual Chrome instance (via Puppeteer, driven ad hoc — not a project dependency) — clicking through the real flow, not just asserting on isolated components. That practice has caught real bugs no unit test would have: the visual builder's own null-array crash and snap-trail mislabeling, a layout bug where the builder's bot palette silently couldn't scroll past 11 bots (a CSS Grid row sizing itself to fit content instead of the viewport), and a pre-existing `email-drive-file` timeout bug (its `max_runtime_secs: 60` guardrail was too short for its own approval gate to ever be answered in time).

## License

MIT

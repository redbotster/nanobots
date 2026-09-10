# Nanobots — Product Blueprint

*Legos for AI. Snap micro-agents together, run them anywhere, keep the keys in 1Claw.*

Version 0.1 · September 2026 · Built on the 1Claw Platform API (`api.1claw.co`, docs at `docs.1claw.co`)

---

## 1. Concept in one screen

| Term | What it is | Maps to (1Claw) |
|---|---|---|
| **Nanobot** | The smallest useful unit of AI work: one harness, one job, typed inputs and outputs. Runs as a bare-minimum container. | One `agent` (+ optional `runtime`) |
| **Port** | A typed input or output on a nanobot. Ports are how context flows. Output ports of one bot snap onto input ports of the next. | `workflow_spec` step output / `{{steps.<name>.output}}` |
| **Snap** | A connection between an output port and an input port. The "stud" on a Lego brick. | Template variable reference in the downstream step |
| **Nanoswarm** | A saved graph of nanobots snapped together, plus shared settings (provider, model, guardrails, schedule). Expressible as one YAML file. | One bootstrap `template` + one or more `automations` |
| **Harness** | The agent loop that drives the bot: `claude-code`, `opencode`, `openclaude`, `hermes`, `openclaw`, or `bare` (no LLM loop; deterministic steps only). | `runtime.template` where 1Claw supports it |
| **Service** | An external system a bot reads or writes: Gmail, Google Drive, Slack, GitHub, HTTP. | OAuth connected account → execution `binding` |
| **Guardrails** | Rules the bot cannot cross: PII policy, injection threshold, allowed providers, spend caps, approval gates. Enforced by 1Claw, not by the bot. | `shroud_config`, spend policy, `approval_request` step, Cedar/OPA policy |

The design principle behind all of it: **a novice should be able to build a working swarm without ever seeing a credential, a cron expression, or a JSON object.** A developer should be able to `git diff` the exact same swarm as YAML.

---

## 2. Configuration spec

Two file kinds, one schema family. Both are Kubernetes-flavoured (`apiVersion` / `kind` / `metadata` / `spec`) so they feel like Helm charts to developers and can be applied by a k8s operator later without redesign.

### 2.1 `Nanobot` — a single brick

```yaml
apiVersion: nanobots.dev/v1alpha1
kind: Nanobot
metadata:
  name: recap-emails-to-pdf
  version: 0.3.0
  description: Summarise unread Gmail since last run and save a PDF recap to Drive.
  tags: [email, reporting, gmail, drive]
  author: nanobots-community
  license: MIT

spec:
  # Which agent loop drives this brick. "bare" = deterministic steps only.
  harness:
    type: openclaw            # claude-code | opencode | openclaude | hermes | openclaw | bare
    version: "^1.4"
    entrypoint: ./bot.md      # harness-specific instructions (system prompt / SKILL.md style)

  # Model settings. Everything here is a default the swarm can override.
  model:
    provider: anthropic       # resolved through 1Claw Shroud; the bot never holds a key
    name: claude-sonnet-4-6
    max_tokens: 4000
    temperature: 0.2

  # External systems. Each becomes an OAuth binding on the 1Claw agent.
  services:
    - id: gmail
      provider: google
      scopes: [gmail.readonly]
      required: true
    - id: gdrive
      provider: google
      scopes: [drive.file]
      required: true

  # Typed ports. This is the Lego geometry.
  ports:
    inputs:
      - name: since
        type: datetime
        default: "{{memory.last_run_at | default: now-24h}}"
      - name: label
        type: string
        default: INBOX
    outputs:
      - name: recap_pdf
        type: file            # file | string | json | datetime | list<...>
        mime: application/pdf
        description: The generated recap document.
      - name: recap_json
        type: json
        schema: ./schemas/recap.json
      - name: drive_file_id
        type: string

  # What the bot actually does. Compiles 1:1 to a 1Claw workflow_spec.
  steps:
    - name: fetch
      type: service.call
      service: gmail
      op: messages.list
      params: { q: "after:{{inputs.since}} label:{{inputs.label}}", max: 200 }
    - name: summarise
      type: ai.generate
      prompt_file: ./prompts/recap.md
      inputs: { messages: "{{steps.fetch.output}}" }
      output: recap_json
    - name: render
      type: transform.render
      template: ./templates/recap.html
      to: pdf
      output: recap_pdf
    - name: upload
      type: service.call
      service: gdrive
      op: files.create
      params: { folder: "{{swarm.vars.recap_folder}}", file: "{{outputs.recap_pdf}}" }
      output: drive_file_id
    - name: remember
      type: memory.put
      key: last_run_at
      value: "{{run.started_at}}"

  # Constraints the bot declares about itself. 1Claw enforces the ones it can.
  guardrails:
    pii: redact
    injection_threshold: 0.7
    max_runtime_secs: 240      # stay under 1Claw's 300s automation cap
    network_egress: [googleapis.com]
    writes_allowed: [gdrive]   # gmail is read-only for this brick

  # Container footprint. "bare minimum" is the default, not the exception.
  resources:
    preset: small               # small | medium | large (1Claw runtime presets)
    memory: 256Mi
    cpu: 250m
    image: ghcr.io/nanobots/harness-openclaw:1.4-slim
```

Ports are the whole trick. A bot with `outputs.drive_file_id: string` can snap onto any bot with an `inputs.*: string` port. The UI only offers snaps that type-check.

### 2.2 `Nanoswarm` — bricks snapped together

```yaml
apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: daily-email-recap
  description: Every weekday morning, recap my inbox to a PDF in Drive, then email me the link.
  owner: me@example.com

spec:
  # Swarm-wide defaults. Every bot inherits unless it overrides.
  defaults:
    model: { provider: anthropic, name: claude-sonnet-4-6 }
    guardrails:
      pii: redact
      injection_threshold: 0.7
      daily_budget_usd: 5
      approval_required_for: [email.send]   # human taps approve before any outbound mail
    resources: { preset: small }

  vars:
    recap_folder: "Recaps/2026"
    notify_to: me@example.com

  trigger:
    type: cron
    expr: "0 7 * * 1-5"
    timezone: America/Chicago

  bots:
    - id: recap
      use: recap-emails-to-pdf@0.3.0          # registry ref; or path: ./bots/recap
      inputs:
        label: INBOX
    - id: mailer
      use: email-drive-file@1.1.0
      inputs:
        to: "{{vars.notify_to}}"
        subject: "Inbox recap for {{run.date}}"

  # Snaps: output port -> input port. Type-checked at plan time.
  snaps:
    - from: recap.drive_file_id
      to:   mailer.file_id
    - from: recap.recap_json.headline
      to:   mailer.body_intro

  # Where it runs. Same swarm, three targets.
  deploy:
    target: local            # local | 1claw | kubernetes | apple
    onclaw:
      billing: user_pays
      vault: nanobots-main
```

### 2.3 Values overlay (Helm-style)

`nanoswarm.values.yaml` overrides `spec.vars`, `spec.defaults`, and `spec.deploy` without touching the graph. `nanobots plan -f swarm.yaml -f values.prod.yaml` shows the diff before applying.

### 2.4 Compile targets

The CLI compiles the same swarm to whichever runtime you point it at:

| Target | Output | Notes |
|---|---|---|
| `local` | `docker-compose.yaml` + a `nanobotd` controller | Default dev loop. Secrets still come from 1Claw via agent JWT; nothing in `.env`. |
| `1claw` | Platform bootstrap `template` + `automations[]` + `runtimes[]` | Each bot → agent; each step → workflow step; snaps → `{{steps.<name>.output}}` refs. |
| `kubernetes` | `Nanobot` / `Nanoswarm` CRDs + operator reconciles to Jobs/CronJobs | CRD schema is the same YAML, so `kubectl apply -f swarm.yaml` works. |
| `apple` | `container` CLI invocations (macOS 26+) | For Mac-native local runs without Docker Desktop. |

### 2.5 Step type mapping (Nanobot → 1Claw `workflow_spec`)

| Nanobot step | 1Claw step type | Notes |
|---|---|---|
| `ai.generate` | `ai_generate` | `max_tokens` ≤ 16384 |
| `service.call` | `execute_intent` with an OAuth `binding` | Nanobots ships a small op catalogue per provider (`messages.list`, `files.create`, …) that expands to the raw HTTP call |
| `http.request` | `http` | SSRF-protected by 1Claw |
| `memory.get` / `memory.put` / `memory.search` | same | namespace defaults to the swarm name |
| `transform.render` | *no 1Claw equivalent* | runs inside the bot container (see §4) |
| `approve` | `approval_request` | pauses the run; human decides in the WebUI or 1Claw mobile |
| `if` | `condition` | sub-steps limited to log/http/notify/ai_generate/memory ops |
| `notify` | `notify` | slack / email / webhook |
| `wait` | `wait` | ≤ 30s |

---

## 3. Architecture

### 3.1 The local stack (what ships in `nanobots up`)

```
┌────────────────────────────────────────────────────────────────────┐
│  Browser (desktop / mobile)                                         │
│  WebUI — swarm canvas, bot library, inspector, run log, YAML drawer │
└───────────────▲───────────────────────────────────────────────┬────┘
                │ REST + SSE                                     │ 
┌───────────────┴───────────────────────────────────────────────▼────┐
│  nanobotd  (single Go binary, ~20 MB)                               │
│  ├─ Registry client     search/pull bots (OCI artifacts)            │
│  ├─ Planner             type-checks snaps, builds the run DAG       │
│  ├─ Scheduler           cron / webhook / manual triggers            │
│  ├─ Context bus         passes port values between bots (NATS-lite)│
│  ├─ Compiler            local | 1claw | k8s | apple targets         │
│  └─ 1Claw bridge        agent JWT exchange, Shroud proxy, bindings  │
└──────┬──────────────────┬──────────────────┬───────────────────────┘
       │                  │                  │
┌──────▼──────┐   ┌───────▼─────┐   ┌────────▼──────┐
│ bot: recap  │   │ bot: mailer │   │ bot: …        │   ← one container each,
│ openclaw    │   │ bare        │   │ hermes        │     distroless, non-root,
│ 256 Mi      │   │ 64 Mi       │   │ 512 Mi        │     read-only FS
└──────┬──────┘   └──────┬──────┘   └───────────────┘
       │ HTTPS via 1Claw bindings (the bot never sees a token)
┌──────▼────────────────────────────────────────────────────────────┐
│  1Claw  —  vault (HSM) · agents · OAuth bindings · Shroud (LLM)    │
│           · automations · runtimes · risk engine · approvals       │
└───────────────────────────────────────────────────────────────────┘
```

**Key decisions**

- **Bots are stateless containers; context lives on the bus.** A bot receives its resolved inputs as a JSON envelope on stdin (or `/run/inputs.json`), writes outputs to `/run/outputs/`, exits. That is the entire contract. Any harness that can read a file and write a file can be a nanobot.
- **All LLM traffic goes through 1Claw Shroud.** The bot container has no provider API key. `nanobotd` mints a short-lived agent JWT (`POST /v1/auth/agent-token`) and injects it; Shroud enforces PII redaction, injection scanning, provider allow-lists, and budgets. Guardrails are therefore identical locally and in the cloud.
- **All service traffic goes through 1Claw execution bindings.** Gmail/Drive/Slack calls are `POST /v1/agents/{id}/execute` with a `binding` id. Tokens are refreshed server-side; the bot never holds them.
- **The WebUI is a thin client.** Same UI whether pointed at `localhost:7474` (nanobotd) or `app.nanobots.dev` (hosted control plane). Mobile is a first-class layout, not a breakpoint afterthought: the canvas becomes a vertical stack of bricks with the same snap semantics.

### 3.2 How a swarm is provisioned on 1Claw

1. User signs in with 1Claw (OAuth, PKCE) → Nanobots calls `POST /v1/platform/users/upsert` with `create_sub_org: true` so every user's resources are isolated.
2. Compiler turns the swarm into a bootstrap template: one `agents[]` entry per bot (with `shroud_enabled: true` and the merged `shroud_config`), `policies[]` scoped to `nanobots/<swarm>/**`, `runtimes[]` for harness bots, `automations[]` for the trigger.
3. `POST /v1/platform/connections/{id}/bootstrap` with an `Idempotency-Key` of `swarm:<name>:<content-hash>` so re-applies are safe.
4. For each `services[]` entry, the UI walks the user through `POST /v1/agents/{id}/oauth/connect` (human-only by design — the platform cannot do this silently, which is a feature).
5. Runs and approvals stream back via the platform webhook (`automation.run.failed`, `pending_approval.created`, …) into the WebUI run log.

### 3.3 Security posture

- Zero credentials at rest in the local stack. `nanobots up` with no 1Claw login gives you a demo mode with mocked services and a local Ollama model, nothing else.
- Every bot's `guardrails.network_egress` compiles to a container network policy locally and is reported (not enforced) on 1Claw until 1Claw exposes per-runtime egress control (see §4).
- Every outbound side-effect the user marked `approval_required_for` becomes an `approval_request` step; the run pauses in `awaiting_approval` and the user approves from the WebUI or the 1Claw mobile queue.
- Bots pulled from the registry are OCI artifacts signed with cosign. The planner refuses unsigned bots unless `--allow-unsigned`.

### 3.4 The context bus, concretely

Port values are small. A `file` port carries a content-addressed reference (`nbf://sha256/…`) into a local blob store (or the user's Drive on 1Claw), never the bytes. `nanobotd` resolves references just-in-time when the downstream bot starts. This keeps the bus tiny and lets snaps cross machines: a bot on your laptop can hand a file to a bot on a 1Claw runtime.

---

## 4. Things to ask 1Claw for *before* building around them

These are gaps between what the docs expose today (checked 10 Sep 2026) and what the flow above needs. Each has a workaround so nothing blocks the MVP, but the "right" fix lives in 1Claw.

| # | Need | What the docs show today | Workaround for MVP | Ask |
|---|---|---|---|---|
| 1 | **Gmail scopes on the Google OAuth provider** | Google provider registry lists `openid, email, profile, calendar, drive`. No `gmail.*` scope. The example flow is Gmail-first. | Pass custom scopes on `/oauth/connect` and see if the registry rejects unknown ones; otherwise use a user-supplied Google OAuth app with Gmail scopes. | Add `gmail.readonly`, `gmail.send`, `gmail.modify` to the seeded Google provider. |
| 2 | **Local / self-hosted runtimes** | Runtimes are cloud presets (`small` … `large-cc`) with `template: hermes|openclaw|openclaude`. Nothing about running a runtime on the user's machine. | Local bots run under `nanobotd` and only use 1Claw for secrets, Shroud, and bindings. The `1claw` deploy target uses cloud runtimes. | A "bring-your-own-runtime" registration: a local agent that heartbeats to 1Claw and receives automation runs, so one swarm can span laptop + cloud. |
| 3 | **Typed step outputs** | `{{steps.name.output}}` is untyped; JSON is re-parsed if the string starts with `{`/`[`. | Nanobots type-checks at plan time and validates at run time inside the bot. | Optional `output_schema` on steps so 1Claw can reject bad wiring server-side. |
| 4 | **Automation run timeout (300 s) and `wait` cap (30 s)** | Hard caps documented. A recap over 200 emails plus PDF render may exceed 300 s. | Split long bots into a chain (fetch → summarise → render) with `memory_put` checkpoints between automations. | Per-run timeout up to ~15 min on Pro+, or a "long step" that delegates to a runtime and polls. |
| 5 | **`transform.render` (HTML→PDF) step** | No render step in the workflow catalogue. | Run rendering inside the bot container (Chromium-headless in a `-slim` image). | Server-side `render` step, or a tiny "document" binding. |
| 6 | **Bot-to-bot context handoff across automations** | Steps share context inside one automation. Two automations only share via `memory_*` or webhooks. | Each swarm compiles to a single automation where possible; cross-swarm snaps use `memory_put` + `event` trigger. | A first-class "chain automation" or an `emit` step that triggers another automation with a payload. |
| 7 | **Per-runtime network egress allow-list** | Shroud has `allowed_providers`; nothing for arbitrary egress. | Enforce locally via container network policy; report on cloud. | `egress_allowlist[]` on runtimes. |
| 8 | **Bot registry / marketplace for end-user bricks** | Marketplace lists platform *apps*, not reusable workflow units. Automation presets are fixed (10). | Nanobots hosts its own OCI registry. | Either expose presets as a publishable registry, or let platform apps publish "components" to the marketplace. |
| 9 | **Webhook events for run *started* / *step completed*** | Only `automation.run.failed` is listed for platform apps. | Poll `GET /v1/automations/{id}/runs`. | `automation.run.started`, `automation.run.succeeded`, `automation.step.completed`. |
| 10 | **Model catalogue endpoint** | Chat accepts `provider` + `model` free-text. | Ship a static list per provider in the UI. | `GET /v1/shroud/models` so the picker stays current. |

Recommended order to raise with 1Claw: **1, 6, 9** (they block the demo flow), then **2** (it is the product's headline promise), then the rest.

---

## 5. Go-to-market

### 5.1 Positioning

*Zapier is glue. Agents are labour. Nanobots is the box of bricks in between — small enough to trust, snapped together into something that does your job while you sleep, with the keys locked in a vault you control.*

The wedge is trust, not capability. Solo founders already have access to capable agents; what they lack is a way to give one access to Gmail without lying awake about it. 1Claw's HSM custody, human-only OAuth, and approval gates are the story, and Nanobots is the friendly face on it.

### 5.2 Who first

| Segment | Trigger job | Why they convert |
|---|---|---|
| Solo founders / indie hackers | "Recap my inbox and file it" · "Draft replies to support mail for me to approve" | Already comfortable with AI, allergic to SaaS sprawl, will pay $20–40/mo for something that runs locally. |
| Small agencies & consultancies (2–15 people) | Client reporting swarms: pull data from Drive/Sheets/Slack, write the weekly update, wait for approval, send. | Repeatable across clients = repeatable across swarms. Each client is a values overlay. |
| Developer advocates & AI tinkerers | Publishing bots to the registry. | They create the supply side of the marketplace. |
| Enterprise (later) | Platform teams who want a paved road for agents with policy (Cedar/OPA) and audit. | Same YAML, k8s operator, SSO, private registry. |

### 5.3 Motion

1. **Open-core, local-first.** `brew install nanobots && nanobots up` gives you the full local stack and the WebUI. The hosted control plane, registry pro features, and 1Claw-backed cloud runs are the paid layer.
2. **Ten launch bricks, three launch swarms.** Gmail recap, Drive-to-email, Slack digest, GitHub PR summariser, Sheets-to-report, calendar prep, invoice chaser, lead enricher, meeting-notes filer, competitor watch. Swarms: *Daily email recap*, *Weekly client report*, *Support triage with approval*.
3. **Marketplace flywheel.** Anyone can publish a bot. Bots declare ports, so composability is automatic — no integration work for the publisher. Revenue share on paid bricks in v2.
4. **Distribution.** 1Claw marketplace listing (`GET /v1/platform/marketplace`), Product Hunt, a "build a swarm live" YouTube series, template gallery SEO ("AI email recap to PDF"), and the docs-as-tutorial pattern (every doc page is a runnable swarm).
5. **Community.** Discord with a `#brick-requests` channel; monthly "swarm jam".

### 5.4 Pricing (draft)

| Tier | Price | Includes |
|---|---|---|
| Local | Free (OSS) | Full local stack, unlimited bots, BYO 1Claw account |
| Solo | $24/mo | Hosted control plane, 5 cloud swarms, 2k runs/mo, mobile approvals |
| Studio | $79/mo | 25 cloud swarms, 20k runs/mo, private bots, values overlays per client |
| Enterprise | Custom | SSO, private registry, k8s operator support, Cedar/OPA policy packs |

Runs map to 1Claw automation tier limits; keep Nanobots' caps below 1Claw's so the user never hits the underlying limit first.

### 5.5 Metrics that matter in the first 90 days

- Time from install to first successful run (target < 10 minutes).
- Percentage of swarms that reach an approval gate and get approved (proxy for trust).
- Bricks published by non-team members.
- Local → cloud conversion rate.

---

## 6. Bootstrap prompt

Paste this into Claude Code (or opencode / openclaude) in an empty repo. It is written so the harness plans first, asks about the 1Claw gaps, then builds thin vertical slices.

```
You are bootstrapping "Nanobots", a local-first product for composing micro AI agents
("nanobots") into workflows ("nanoswarms"), with all secrets, OAuth bindings, LLM
routing, and guardrails delegated to the 1Claw Platform API (https://docs.1claw.co).

Read NANOBOTS-BLUEPRINT.md, NANOBOTS-CATALOG.md, and examples/*.yaml in this repo before writing code.
Treat the YAML schemas there as the source of truth.

Goals for this session, in order. Stop and show me a plan after step 1.

1. Repo layout as a monorepo:
   - cmd/nanobotd   — Go 1.23 controller: REST + SSE API, planner, scheduler,
                      context bus, compiler (targets: local, 1claw, kubernetes, apple).
   - cmd/nanobots   — Go CLI: init, add, plan, up, run, save, publish, compile.
   - web/           — WebUI: Vite + React + TypeScript, no component library,
                      Tailwind core utilities only. Mobile-first layout.
   - harness/       — Dockerfiles for bare, openclaw, hermes, openclaude, opencode,
                      claude-code. Distroless where possible, non-root, read-only FS.
   - schemas/       — JSON Schema for Nanobot and Nanoswarm (generate from Go types).
   - examples/      — the two bots and one swarm already present.
   - docs/          — one page per concept, each ending in a runnable swarm.

2. Implement the bot contract first and prove it with the `bare` harness:
   inputs arrive as /run/inputs.json, outputs are written to /run/outputs/<port>,
   exit code 0 = success. Write a conformance test any harness image must pass.

3. Implement the planner: parse Nanoswarm, resolve `use:` refs from ./bots or the
   registry, type-check snaps (output type must be assignable to input type),
   build a DAG, reject cycles. `nanobots plan` prints the DAG and any errors.

4. Implement the local runner: docker-compose generation + execution via the Docker
   API, context bus as an in-process channel with an on-disk blob store for `file`
   ports (nbf://sha256/... references). Stream run logs over SSE.

5. Implement the 1Claw bridge. Use only endpoints documented at docs.1claw.co:
   - Sign in with 1Claw (OAuth code + PKCE), POST /v1/platform/users/upsert with
     create_sub_org: true.
   - Compile a Nanoswarm to a bootstrap template (agents[], policies[], runtimes[],
     automations[] with workflow_spec) and POST /v1/platform/connections/{id}/bootstrap
     with an Idempotency-Key derived from the swarm content hash.
   - Agent JWT exchange (POST /v1/auth/agent-token) and inject it into bot containers.
   - OAuth connect flow (POST /v1/agents/{id}/oauth/connect) surfaced in the WebUI.
   - Receive platform webhooks (verify X-Webhook-Signature) and map them to run events.
   Where the blueprint's §4 lists a gap, implement the documented workaround and add
   a TODO(1claw#N) comment so we can swap in the native feature later.

6. WebUI vertical slice: bot library search → drag two bots onto the canvas → snap
   recap.drive_file_id to mailer.file_id → set provider/model/guardrails in the
   inspector → Run once → watch the run log → Save as nanoswarm → view the YAML drawer.
   Follow the visual direction in web/DESIGN.md (Blue Tron: deep navy void, cyan light
   trails, one display face + one text face). Keep it quiet everywhere except the
   snap connector, which is the memorable element.

7. Build the launch catalog from NANOBOTS-CATALOG.md, in tranche order (A, then B, then C).
   For each brick:
   - Create ./bots/<id>/nanobot.yaml matching the schema, with the ports, services,
     harness, and default guardrails exactly as listed in the catalog table.
   - Write ./bots/<id>/bot.md (the harness instructions) in plain second person,
     under 300 words, with the one-sentence job as the first line.
   - Write one conformance test that feeds fixture inputs and asserts every declared
     output port is produced with the declared type.
   - Any brick whose job includes send / post / pay / delete must include an `approve`
     step before the side effect, and the swarm-level `approval_required_for` default
     must not be able to remove it for bricks 4 and 11.
   For each swarm in the catalog:
   - Create ./examples/swarms/<id>.yaml with the snaps shown in the table, and run
     `nanobots plan` to prove every snap type-checks. Fix the ports, not the planner.
   - Add a gallery entry (name, one-line description, persona, screenshot placeholder)
     to web/src/gallery.json so it appears under "Start from a swarm".
   Where a service in the catalog is missing from 1Claw's OAuth provider registry,
   implement the documented fallback (user-supplied OAuth app credentials, or an
   API-key binding in the vault), and add a TODO(1claw#catalog) comment naming the
   provider so we can swap to native later. Do not skip the brick.

Constraints:
- Never store provider API keys or OAuth tokens on disk or in env; everything goes
  through 1Claw. If a code path would need a raw credential, stop and ask me.
- Every container must run as non-root with a read-only root filesystem.
- No feature without a test. Go: table-driven tests. Web: Vitest + Testing Library.
- Keep the initial dependency list small and list every new dependency in the PR
  description with one line on why.
- After each step, run the full test suite and show me a short summary before moving on.
```

---

## 7. Roadmap

| Phase | Weeks | Outcome |
|---|---|---|
| 0 · Contract | 1–2 | Bot contract, `bare` harness, conformance test, planner with type-checked snaps |
| 1 · Local loop | 3–5 | `nanobots up`, local runner, WebUI canvas + inspector + run log, YAML drawer |
| 2 · 1Claw bridge | 6–8 | Sign-in, bootstrap, Shroud routing, OAuth connect in UI, approvals, webhooks |
| 3 · Launch bricks | 9–10 | Catalog tranches A + B (18 bricks, 6 swarms), registry with signed OCI artifacts — see NANOBOTS-CATALOG.md |
| 4 · Cloud & mobile | 11–13 | Hosted control plane, mobile approvals, `1claw` deploy target GA |
| 5 · Operators | 14+ | k8s CRDs + operator, Apple containers target, Cedar/OPA policy packs |

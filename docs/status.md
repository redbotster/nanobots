# What works today

This repo implements the full 28-brick, 12-swarm launch catalog from
`context/NANOBOTS-CATALOG.md` (Tranches A, B and C), plus the two bricks and
two swarms that predate it and everything added since — **39 nanobots, 18
nanoswarms** — end to end and verified live, not just type-checked.

The short version of the honesty question is the table at the bottom: [what
is real and what is simulated](#whats-real-vs-simulated). The list first.

## Built and exercised

- **The bot contract, planner and Docker-backed local runner.** Bots run as
  real, non-root, read-only-filesystem containers, wired to each other's
  outputs across separate containers, streamed live over SSE.
  `nanobots conform` proves every bot honors the contract without Docker;
  `nanobots plan` type-checks every swarm's snaps against real port types
  ([architecture.md](architecture.md)).
- **A real [1Claw](https://docs.1claw.co) bridge.** Human API key to bearer
  token exchange, agent creation and deletion, Shroud chat completions,
  vault secrets, agent memory, approval requests and a Browser Bridge client
  — all exercised against the real API, not mocked
  ([oneclaw-bridge.md](oneclaw-bridge.md)).
- **Seven real, standalone direct-service clients** for what 1Claw does not
  natively cover: Google (Gmail, Drive, Sheets, Calendar), Slack, GitHub,
  Stripe, HubSpot, X, LinkedIn — each dispatched from `internal/step.LiveDeps`
  once a bot's service is switched off `connection: demo` **and** a human has
  connected the account — plus a credential-free `web.fetch` step for reading
  public pages. None is the default connection for any bot out of the box.
- **An AI composer** that turns a plain-English request into a validated
  draft swarm, and escalates to [the foundry](foundry.md) when the catalog
  genuinely cannot do what was asked.
- **A WebUI** (Vite + React + TypeScript + Tailwind + Radix) with a
  basic/advanced toggle, a searchable bot library grouped by service with a
  per-service demo/live switch, a Team view, a swarm gallery showing at a
  glance how much of each swarm is live, a live run viewer with SSE log
  streaming and inline approvals, browser notifications when something needs
  you, a Settings page where connecting a service is a button or a pasted
  token, and a visual builder ([builder.md](builder.md)).
- **Per-bot customisation.** Every LLM bot takes an optional `instructions`
  port with a suggestion written for its job, editable from its card or
  overridable per swarm. It shapes how a bot works, never what it is allowed
  to do ([bot-contract.md](bot-contract.md)).
- **Any model provider, 1Claw first.** `ai.generate` and the composer go
  through one interface with four backends: 1Claw Shroud (the default, and
  the only one with a budget ceiling, PII redaction and injection screening),
  Anthropic, Gemini, and OpenAI-or-anything-speaking-its-format. A bot asking
  for a model the backend cannot serve gets a substitute, logged into the run
  rather than swapped silently ([llm.md](llm.md)).
- **A fanned-out bot's items run at once.** "For each of these, do that" is
  independent by definition, and ran one after another anyway —
  `supervisor-review` spent 26.5s of a 50s run on three reviewers reading the
  same work in sequence. Same cap as a wave; about a quarter off that swarm,
  measured over three pairs of live runs
  ([parallelism.md](parallelism.md)).
- **A swarm's independent branches run at once.** The planner always knew the
  DAG; the runner used to walk it one bot at a time. Five of the sixteen
  catalog swarms have a wave wider than one ([parallelism.md](parallelism.md)).
  The other eleven are straight chains and gain nothing, which is worth
  saying rather than implying a general speed-up.
- **A first screen that says what is next.** Three steps that tick themselves
  off from real state — point it at a model, run one on example data, connect
  an account when you want it real — and the card removes itself once they
  are done. Explicitly not a wizard: running on example data is a legitimate
  way to use this, not a degraded one.
- **Demo data never passes for real.** Every bot ships on `connection: demo`,
  so the default run succeeds with plausible invented output. The log marks
  each such call, and the run says which services were answered from example
  data with a button to connect an account
  ([connections.md](connections.md)).
- **A real run can become a bot's test data.** Fixtures are what
  `nanobots conform` replays offline, and they were hand-written — a guess at
  what a model returns, which drifts. A succeeded run offers back exactly
  what it got, marked new or replacing, side by side with what is committed
  ([fixtures.md](fixtures.md)). Approvals are never recorded, because a
  pinned "approved" would turn a gate into a rubber stamp.
- **A failure at the edge does not lose the run.** A bot can be marked
  `on_error: continue`, and everything downstream of it is skipped rather
  than run against missing inputs ([error-policy.md](error-policy.md)).
  `get-paid` used to send every reminder and *then* fail the whole run
  because it could not post a Slack summary.
- **Bots remember between runs.** Local key/value by default, or a
  recall-capable backend that answers questions in plain language —
  `inbox-triage` recalls what you have actually treated as urgent
  ([memory.md](memory.md)).
- **An approval says what approving does.** The prompt shows the risk tier
  and what the bot will write to — `sender will write to gmail. Declining
  stops the run here.` — instead of only the subject line
  ([approvals.md](approvals.md)). "Skip" is now "Don't approve", because
  declining ends the run rather than skipping a step.
- **A wall of red reads as one fact.** The Runs page collapses consecutive
  identical failures ("and 42 more that failed the same way"), filters by
  All / Needs you / Failed / Succeeded, and the waiting-approval banner opens
  the run instead of just mentioning it.
- **A bot cannot fetch your own machine.** `web.fetch` takes a URL from a
  bot's inputs and runs inside nanobotd, so it used to reach loopback — a
  swarm pointed at `127.0.0.1:7474` returned the daemon's own webhook token
  into its output. Loopback, cloud-metadata, private and multicast addresses
  are refused now, checked in the dialer at connect time so a hostname
  resolving to 127.0.0.1 and a redirect are both caught
  ([connections.md](connections.md)).
- **The catalog does not sound like AI.** Every prose bot's prompt states the
  house voice, and no bot's demo output contains an em dash. Both are
  asserted by tests, because the `tone` bot existed to strip exactly the
  habits the rest of the catalog was modelling.
- **The run log is actually live.** It used to sit on three lines for sixteen
  seconds and then produce five at once, because the in-container agent
  buffered every line and wrote them on exit ([runs.md](runs.md)).
- **You can stop a run.** A hanging bot used to hold its container for the
  whole ceiling with watching as the only option. Stop cancels the run's
  context and `docker kill`s the container, releases any approval gate, and
  reports itself as *stopped* rather than *failed* everywhere that reads it —
  including the scheduler's circuit breaker, since five runs you stopped by
  hand are not a swarm that is broken ([runs.md](runs.md)).
- **An idle tab costs almost nothing, and so does moving around.** `GET
  /api/runs` was 91KB polled every two seconds and near-always identical; an
  ETag got 7 polls over 14 seconds down from 638KB to 2.1KB, and then the
  poll itself was replaced with an SSE stream (`GET /api/runs/events`) that
  sends a snapshot once and a delta only when a run's state actually
  changes — measured the same way, in a browser: 0 requests, 0 bytes over
  the same 14-second idle window ([runs.md](runs.md)). The bot catalog and
  the swarm list are held the ETag'd way for navigating rather than polling,
  and a five-page browse went from 335.9KB over 32 requests to 123.6KB over
  30 ([runs.md](runs.md)). The bigger one was the app itself:
  the embedded UI was served uncompressed and untagged, so a cold load was
  387.7KB and a reload paid it again. Compressing and tagging it, then
  splitting the pages nobody has navigated to out of the main chunk, takes a
  cold load to 71.1KB — and a reload asks for nothing
  ([hosting.md](hosting.md)). The slowest thing left was not bytes at all:
  `GET /api/connections` reads eight secrets — against a throttled 1Claw
  vault when that measurement was taken — and four screens fetch it on
  mount, including the landing page's. A first page load went from 3.5s to
  0.99ms ([connections.md](connections.md)).
- **What you ask for gets scheduled.** Composing "every friday summarise my
  overdue invoices" used to produce a swarm that described itself as weekly
  and would never fire. The composer emits cron now, the builder shows it
  described back to you as you type, and an expression the scheduler cannot
  parse is refused rather than saved ([scheduler.md](scheduler.md)).
- **A schedule that never works stops trying.** After five consecutive
  failures the scheduler pauses it, and the card says why with the error and
  a **Try it again** button. Found on a real machine: one swarm had failed 85
  times because Slack was never connected, 41 of those runs holding a
  container open for the full 30-minute ceiling
  ([scheduler.md](scheduler.md)).
- **Webhook triggers actually fire.** `POST /webhooks/{swarm}` starts a run
  with the body as `{{trigger.payload}}`, token-guarded, answering 202 rather
  than holding the sender open through an approval gate
  ([webhooks.md](webhooks.md)).
- **A swarm can leave the machine.** `nanobots export` bundles one and
  `import` refuses before writing anything if a bot is missing
  ([sharing.md](sharing.md)). Bots are named, not carried, so nobody ends up
  running a silent fork. Bundles carry no credentials.
- **The CLI can check the whole catalog.** `nanobots conform bots` runs all
  39 bots' fixtures and `nanobots plan` type-checks all 18 swarms, one line
  each, continuing past a failure so you see everything that broke.
- **1Claw's own telemetry, where you already look.** The Settings System
  block gains a Posture row — score, open threats, and agent usage against
  your plan's cap, which is the actionable one: every bot name this repo runs
  takes an agent slot ([oneclaw-bridge.md](oneclaw-bridge.md)).
- **What it cost is visible.** `nanobots spend` and the Model section of
  Settings read the same 1Claw token-billing figure. On a direct provider key
  it says the spend is not metered here rather than showing a confident
  `$0.00` that actually means "no idea" ([llm.md](llm.md)).
- **One place to wire up a real account.** `nanobots connectors` uses 1Claw's
  connector presets, so a provider is a preset rather than another Go package
  and another refresh-token dance ([connectors.md](connectors.md)).
- **It survives a reboot.** `nanobots service install` writes a per-user
  LaunchAgent so nanobotd keeps running, which is what makes twelve cron
  triggers more than aspiration ([scheduler.md](scheduler.md)).
- **Run history that survives a restart.** Every finished run is kept in
  `~/.nanobots/nanobots.db` (SQLite, migrated once from the JSON files this
  used to be), capped at 1,000. A run killed mid-flight by a restart is
  restored as failed rather than sitting in the list as "running" forever
  ([run-history.md](run-history.md)).

## Bots with no swarm to show them

Eight of the thirty-nine bricks were in no swarm at all, which for a product
whose pitch is "these snap together" is the pitch going undemonstrated.
`watch-the-competition` and `thread-from-an-idea` take three of them
(`competitor-watch`, `tone`, `x-thread-writer`). The other five are honest
gaps rather than oversights:

- `newsletter-drafter` and `quote-builder` need a shape bridge that does not
  exist. A snap is one field to one field, and only a bot's own steps can
  build a new shape from its own data — so nothing in the catalog can turn
  one file's outputs into the `list<string>` pair a newsletter wants.
  Forcing the snap is the wart `lead-to-meeting` and `meeting-to-action`
  already apologise for in their headers; two is enough.
- `review-responder` needs Google Business Profile, for which no client
  exists ([connections.md](connections.md)). A swarm built on it could never
  run against a real account.
- `linkedin-dm-triage` needs LinkedIn messages, which have no API path at
  all — not a todo, a wall.
- `linkedin-comments` reads the comments on one post, and deduping repeat
  reads needs a filter the step language does not have. `listen-and-reply`
  says so in its own header rather than shipping a branch that fails for
  most people.

## Not built yet

The `kubernetes` and `apple` compile targets. A real dynamic agent loop *as
a bot harness* — today's bot harnesses run a fixed, pre-written step list,
not an LLM deciding what to do ([harnesses.md](harnesses.md)). Two different
kinds of dynamism exist now, neither of them a harness a bot declares:
`agent.loop` ([agent-loop.md](agent-loop.md)) is a single *step* where the
model decides which of a declared set of tools to call, iteration by
iteration — built and unit-tested, not yet used by any catalog bot, so it
hasn't been proven doing real work end to end the way Team has. Team
([team.md](team.md)) runs a real coding-agent CLI in its own persistent
workspace, and Lab ([lab.md](lab.md)) talks to it from one chat tab. A real
OAuth integration for Google Business Profile, so `review-responder` stays
on `connection: demo`. The hosted multi-tenant control plane.

## What's real vs. simulated

Being explicit about this matters more here than in most projects, because
so much of what nanobots *is* is "the layer that hides whether something is
real". From a user's perspective a bot's Gmail call should look the same
whether it is live or fixture data, which makes it easy to accidentally
paper over what is actually happening. So, plainly:

| Real | Simulated / not yet |
|---|---|
| One binary serves the WebUI and the API on a single port (`make build && nanobots up`), and 34 of 39 bots run without Docker. `v0.1.0` publishes binaries for six platform/arch pairs and a multi-arch `ghcr.io/redbotster/nanobots` image, which is what `nanobots deploy 1claw` now runs by default | `brew install` and `npx nanobots` still do not work: the Homebrew tap repository does not exist and the npm package is unpublished. Both configurations are written and validate; each is one secret away ([hosting.md](hosting.md)) |
| 1Claw Human API, Shroud, vault secrets, agent memory, approval requests | 1Claw's execution-intent bindings (`internal/oneclaw.Execute`) assume a binding already exists on the agent — provisioning one from a `nanobot.yaml` service is not wired up |
| Docker execution for the 5 browser bots: non-root, read-only fs, real container-to-container I/O wiring, no network interface at all for a bot that declares no egress and calls nothing, and an **enforced** `network_egress` allowlist for one that does — a forward proxy Chrome is pointed at, checking the same list `web.fetch` already checks ([harnesses.md](harnesses.md)) | The proxy still runs on a routable bridge network rather than one that can *only* reach it — safe today because a bot's declared steps never run code other than this repo's own controlled interpreter, but a future harness running less controlled code would need a per-run network with no other route out |
| The step interpreter, for every harness type, incl. `web.fetch` and PDF/PNG rendering | The *dynamic agent loop* a harness name like `openclaw` implies — every bot harness today runs the same fixed, pre-written `spec.steps` list ([harnesses.md](harnesses.md)). Team ([team.md](team.md)) is a real dynamic loop, just not one a bot's own harness runs |
| `agent.loop` — real tool-calling through Shroud (any provider it's configured to reach — verified live with both Anthropic and Gemini, twice independently) and direct Anthropic; every tool call dispatches through the same `deps.ServiceCall`/`WebFetch`/`MemoryGet`/`MemoryPut` a fixed step uses, and a write-capable tool is held behind the same `deps.Approve` gate `email-send-approved` already uses inline. `lead-enricher@0.2.0` uses it for real — a real Shroud call, a real HubSpot-shaped tool call, a real answer reasoning over the result ([agent-loop.md](agent-loop.md), `docs/1claw-feature-requests.md` #13) | `internal/llm.Gemini` and the generic OpenAI-compatible backend don't implement tool-calling as *direct* backends (only reachable via Shroud today). No spend ceiling: nothing reports a per-call cost to check one against. No `transform.*` tools |
| Team — a persistent, role-scoped Claude Code or Gemini CLI agent in its own git worktree, and Lab, one chat tab that delegates to it or answers directly ([team.md](team.md), [lab.md](lab.md)). A real delegation, streamed live into the chat, is verified end to end with the Gemini engine | Claude Code (Team's default engine) is only mechanically verified — the one available API key had no credit. No cross-agent memory, no conversation persistence across a restart, and Lab's "what's a swarm doing" tool reads a Team role's git log, not a swarm's own run history |
| The Google OAuth client (`internal/google`) — real PKCE flow, real REST calls (Gmail, Drive, Sheets, Calendar), unit-tested against fake servers | No bot ships with a non-demo Google connection by default — every bot's Google service is `connection: demo` until a human deliberately flips it ([connections.md](connections.md)) |
| The Slack/GitHub/Stripe/HubSpot clients — real REST calls, unit-tested against fake servers; `notify`'s Slack delivery is genuinely wired once connected | Google Business Profile replies (`review-responder`) need a dedicated integration, and stay on `connection: demo` |
| The X and LinkedIn OAuth2+PKCE clients and the shared `internal/oauth2pkce` core — real token exchange and refresh, real posting calls, wired end to end into `post-publisher` and Settings | Live end-to-end posting has not been exercised against a real X/LinkedIn developer app in this build, only against fake test servers |
| The AI composer — a real Shroud call, a real planner validation pass, a real hydrate-into-the-builder handoff | The composer never auto-saves or auto-runs; a hallucinated bot id or type mismatch surfaces as a normal validation error for the human to see, by design |
| The unified Connect UI (Settings) — real writes and reads through whichever secrets backend is configured (1Claw vault, OS keychain, or encrypted local file — [secrets.md](secrets.md)), a "Secrets" status row naming which one, real Google OAuth kicked off server-side | Connections are always whole-port-to-port in the builder and the composer; a swarm's trigger/vars/deploy config has no UI yet; canvas layout is not persisted |
| Approvals — a run blocks, flips to `awaiting_approval`, and waits for a real decision from the WebUI, the CLI, or 1Claw's own queue, which one shared agent opens it in; first answer wins ([approvals.md](approvals.md)) | A local answer still leaves the mirrored 1Claw approval pending rather than cancelling it — 1Claw's API gained a cancel endpoint after this was first built, but nothing calls it yet; and where 1Claw delivers the question — push, email, dashboard — is your account's setting, not this repo's ([1claw-feature-requests.md](1claw-feature-requests.md)) |
| Per-item fan-out *and* the join back — a `.*` snap runs a downstream bot once per list item with one approval covering the batch ([fan-out.md](fan-out.md)); `get-paid` uses both | `content-engine` and `thread-from-an-idea` still snap `.0` — a product decision, not a missing primitive: fanning either would publish every brainstormed idea in one run instead of "the first one, scheduled across the week" ([fan-out.md](fan-out.md)) |
| Basic/advanced mode toggle — a real, tested UI gate | Purely a UI-visibility gate; it never changes what actually executes |

**A real constraint hit repeatedly while building this**: 1Claw vaults can
require passkey verification before `GetSecret` succeeds, depending on the
account's own vault security tier — a 403 `"Passkey verification required to
access vault secrets"` from 1Claw itself, not a bug here. A connected
credential can sit in the vault but be temporarily unreadable until a human
unlocks it with their passkey in a browser. Bots that only need Shroud or
`web.fetch` are unaffected, which is most of the hero path. A run that hits
this pauses (`awaiting_unlock`, distinct from `awaiting_approval` in the
run status and in the run log) and retries automatically every 30 seconds
rather than failing outright or burning the bot's own `retry:` budget on
something that isn't its own transient failure ([connections.md](connections.md)).

## Keeping this page honest

Several of the numbers above check themselves — the bot and swarm counts,
the cron count, the wave count, the test count, and that every doc is
reachable from the README all fail a test when they drift
([testing.md](testing.md)). A claim nobody can check is a claim that rots.

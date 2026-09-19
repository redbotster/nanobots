# 1Claw feature requests

Things nanobots needs from 1Claw that do not exist yet, what each one
blocks, and the honest workaround shipped in the meantime.

Every entry was checked against `https://api.1claw.co/openapi.json` (526
paths as of the most recent re-check, up from 499) or a live call, not
against the prose docs — the prose doc set currently reports several
shipped features as absent, so it is not a safe source for "does this
exist".

The spec is not proof either — or wasn't, for memory search: #12 was in the
spec and answered every request while matching nothing, until 1Claw fixed
the matching itself. Before building on an endpoint, call it with real
data and check the answer, not the status code. And a "does not exist yet"
entry here is not permanent, for two different reasons: #1, #3, #9, #11 and
#12 went stale because 1Claw shipped the feature after this was written;
#13 was wrong from the start — it inferred an API limit from what
nanobots' own client happened to send, and a live production check found
the real capability was there the whole time. Either way, this file gets
re-checked against the live API rather than trusted as a fixed backlog.

Format: **what we need** / **what it blocks** / **what we do instead**. A
resolved entry keeps its number and switches to **resolved** / **what this
still blocks** so the history of what changed stays visible instead of
disappearing when the gap closes.

---

## 1. ~~Connector presets for Stripe, HubSpot, LinkedIn, Drive, Google Business Profile~~ — shipped

**Resolved.** `GET /v1/connectors/presets` returned nine when this was
written. Re-probed live: it now returns fifteen, and all five named here are
among the new ones — `stripe`, `hubspot`, `linkedin`, `google-drive`,
`google-business` — plus a generic `api-token` preset (any bearer-token
HTTPS API, bound to one host) that wasn't asked for but covers the same
shape of gap. Full detail (scopes, `base_url`, `allowed_hosts`) checked for
each, not just the slug's presence.

**What this still blocks.** Nothing at the API level. Migrating
`internal/stripe`, `internal/hubspot`, `internal/linkedin` (608 lines) and
the Drive portion of `internal/google` (1109 lines) onto these presets is
real, separate work — swapping a hand-rolled client for a binding changes
where the credential lives and how a bot calls it — and hasn't happened
yet. Recorded here as done at the platform level; the migration is tracked
as ordinary backlog, not as a 1Claw gap.

---

## 2. Shared or first-party OAuth apps

**What we need.** A way for a user to connect Google, X or LinkedIn without
registering their own OAuth application and pasting a client ID.

**What it blocks.** The five-minute first run. Connecting Google today means
a trip to Google Cloud Console, which is the single largest drop-off in
setup, and `docs/connections.md` has to explain it.

**What we do instead.** `oauth_native` with a user-supplied client ID, and a
setup section in the README. Connector presets avoid this for the nine
services that have them, which is the argument for request #1.

---

## 3. ~~Cancelling or withdrawing an approval request~~ — shipped

**Resolved.** `POST /v1/approvals/{approval_id}/cancel` exists now — the
same resource family as `/v1/approvals/{approval_id}/decide`, not a
workaround via the platform-executed-actions family this entry originally
pointed at.

**What this still blocks.** Wiring it in. `internal/runner/approver.go`
still leaves the mirror pending and lets it expire, exactly as before; this
entry only records that the endpoint we needed showed up, not that the
build calls it yet. Tracked as ordinary backlog.

---

## 4. Cheap ephemeral or child agents

**What we need.** Agents that do not each consume a plan-capped slot, or a
documented child-agent concept under one parent.

**What it blocks.** Per-bot isolation. This account sits at 27 of 50 agents,
26 of them created by nanobots, one per bot name. Any design that wants an
agent per bot runs out.

The approval mirror is what this looked like in practice. Opening a 1Claw
approval requires an agent, and the obvious shape — the asking bot's own —
would have meant four more agents for `approve`, `email-drive-file`,
`email-send-approved` and `post-publisher`. It uses one shared `nanobots`
agent instead, which is cheaper and loses nothing: the question is addressed
to you either way.

**What we do instead.** Agents are keyed by guardrail profile rather than by
bot (`docs/oneclaw-bridge.md`), which is what an agent actually is from
1Claw's side: `shroud_config` is set per agent and the chat body carries no
per-call override. The whole catalog wants two, plus four fixed ones.
Genuine per-bot isolation still waits on this request.

---

## 5. External event triggers for Automations

**What we need.** `trigger_type: event` accepts vault and policy lifecycle
events. We need external sources: a new Gmail message, a new Drive file, a
new Stripe invoice.

**What it blocks.** `drive-watch` and the planned `gmail-watch`,
`stripe-watch`, `github-watch` bricks. A swarm cannot start from "something
happened out there" without us polling for it.

**What we do instead.** Poll on a cron and dedupe through durable memory
(Phase 3 item 21). Correct, and it costs an API call per interval per watch
rather than zero.

---

## 6. A run budget longer than 300 seconds, or a hand-off step

**What we need.** Either an Automation run timeout longer than the
documented 300 seconds, or a first-class "hand off to a runtime and return"
step so a long wait does not hold the automation open.

**What it blocks.** Moving scheduled swarms to Automations. The `http` step
type says, verbatim: *"Bounded by the 300s whole-run timeout rather than a
per-step one."* Seven of the sixteen catalog swarms pause for a human
approval, and people are not reliably back within five minutes.

**What we do instead.** The local scheduler stays the default. Automations
are opt-in, for swarms that do not wait on a human. See `docs/scheduler.md`.

---

## 7. Creating an execution-intent binding from a declarative spec

**What we need.** To turn a `nanobot.yaml` service block into a binding
without a human clicking through a dashboard —
`POST /v1/agents/{id}/bindings` from a spec, with `allowed_hosts` taken from
the bot's declared `network_egress`.

**What it blocks.** `nanobots connect` provisioning what a bot declares.
Bindings are also human-only to create (agents get 403), so the composer
cannot propose a bot that needs one and have it work.

**What we do instead.** The CLI drives it with the user's own credential and
prints what it will create first. Ship that; revisit if the API gains a
declarative form.

---

## 8. Declarative Charts over the API

**What we need.** An endpoint for `chart.yaml` apply/export. No path
matching `chart` exists among the 499 in the OpenAPI spec.

**What it blocks.** Phase 2 item 16: a shared swarm bundle carrying its full
permission surface (agents, policies, connectors, bindings) for review.

**What we do instead.** Nothing yet. Needs confirming whether charts are
CLI-only or unshipped before building against them.

---

## 9. ~~A TTL / scratch memory tier~~ — it exists, and this entry was wrong

**Withdrawn.** This said "there is no TTL tier". `PutMemoryRequest` carries
`ttl_seconds`, and `MemoryEntry` carries `ttl_expires_at`. Probed live:

```
PUT /v1/agents/{id}/memory/probe-ns/ttl-probe  {"value":"scratch","ttl_seconds":60}
GET /v1/agents/{id}/memory/probe-ns
  -> ttl-probe   tier=durable  ttl_expires_at=2026-09-17T12:28:04Z
     obs-live-1  tier=durable  ttl_expires_at=null
```

So a short-lived entry is one field on the write nanobots already makes.
Nothing in this build uses it yet — no bot has scratch state that should
expire — and it is recorded here rather than built for that reason.

The wrong version of this entry came from reading the prose docs. Same
mistake as the approvals endpoint, in the same week.

---

## 10. Listing approvals as the agent that created them

**What we need.** `GET /v1/approvals` is human-only; an agent gets 403. An
agent can create an approval and poll one by id, but cannot enumerate its
own.

**What it blocks.** Reconciling state after a restart. If nanobotd dies
between creating a mirror and recording its id, that approval is
unreachable to us.

**What we do instead.** Persist the approval id in run history at creation
time, and poll by id. Loses only the runs whose history was already lost.

---

## 11. A runtime template that runs a plain binary, and a way to get files into a runtime

**First half resolved.** `GET /v1/runtimes/templates` returned nine
language-runtime and agent-framework templates when this was written.
Re-probed live: there are now ten, and the new one is `binary` — "Run a
compiled program: a release asset from `BINARY_URL` (SHA-256 pinned) or a
startup command after cloning a repo. 1Claw CLI and sidecar included." That
is exactly the gap named here.

**Second half, still open.** No dedicated file-transfer API for a running
runtime exists yet — `binary`'s own "clone a repo" path is the closest
thing, and covers the same need a different way (push to a repo the
runtime clones, rather than push files to a running one), but it hasn't
been tried against a real deploy.

**What this still blocks.** `nanobots deploy 1claw` doesn't use either yet.
Wiring the CLI to offer `binary` as a template, and to push this repo's own
swarms via a clone rather than `--image`, is real work that hasn't started.
Recorded here as a platform gap now half-closed; the CLI change is ordinary
backlog.

---

## 12. ~~Memory search that matches something~~ — shipped, and wired in

**Resolved.** `POST /v1/agents/{id}/memory/search` used to answer every
non-empty query with nothing, on the account this was first probed against.
Re-probed after 1Claw shipped a fix, same shape of test, one entry in the
namespace:

```
PUT    .../memory/probe-ns2/search-probe  "refunds always get escalated to a human" -> stored
search {"query":"refunds"}                                -> 1 result, score 0.95
search {"query":"escalated to human"}                     -> 1 result, score 0.90
search {"query":"refunds always get escalated to a human"} -> 1 result, score 1.0
search {"query":"what happens with refund requests"}       -> 0 results (no shared words)
```

Exact and partial-word queries now score and rank real matches. The last
line is the honest limit that's left: this is **lexical** matching, not
semantic — a paraphrase sharing no words with the stored text still finds
nothing, so it is not the same capability Honcho's dialectic answer is.

**What this unblocked.** `memory.OneClaw` now implements `Recaller` —
`Remember` stores each observation under a generated key, `Recall` returns
the matching stored text verbatim (not a synthesized sentence, since the
API doesn't perform that step). `docs/memory.md` and
`internal/api/server.go`'s `memoryStatus` were updated in the same change:
the latter had a now-stale hardcoded "1claw never recalls" that would have
kept reporting `memory_recall: false` to the UI even after this landed,
which is exactly the kind of claim this file exists to catch. `inbox-triage`,
`support-triage` and `draft-replies` get real recall on a 1Claw-backed
deployment now; Honcho remains the other recall-capable backend for anyone
who wants synthesized answers over lexical retrieval.

---

## 13. ~~Tool-calling through Shroud~~ — it already works, this entry was wrong

**Withdrawn.** This said `POST https://shroud.1claw.co/v1/chat/completions`
had no way to declare tools, based on `internal/oneclaw.ShroudClient.Chat`
only ever sending one message with no `tools` field — which is a true
description of nanobots' own client and a wrong inference about the
endpoint behind it, the same mistake this file's own header now warns
about making twice in one week.

Verified against production with a real funded key: `tools` +
`tool_choice: auto` on `/v1/chat/completions` returns `finish_reason:
"tool_calls"` with real `tool_calls[0].function` populated; a
`role: "tool"` message with `tool_call_id` round-trips and the model
answers from the tool's own output; `stream: true` forwards `tool_calls`
delta chunks correctly. Shroud forwards the request body unmodified (the
only field it ever strips is Anthropic's `context_management`) and already
parses `tool_calls` in both directions for its own inspection —
`shroud_config.tool_call_inspection`'s `allowed_tool_names` /
`denied_tool_names` / argument scanning govern it per agent. Billing and
guardrails apply to the whole request either way, funded or BYOK.

**The one real requirement, found the hard way**: `X-Shroud-Provider` is
required — a funded probe without it 400s, which is almost certainly what
this entry's own probe hit before concluding the feature was missing.
`internal/oneclaw/shroud.go` already sends this header for `Generate`'s
existing single-shot call, so a tool-calling call built the same way
should carry it forward without a new mistake to make here.

**What this means for v3 Phase 3's `agent.loop`.** It is not blocked.
`internal/llm.Generator`'s single-prompt-in/single-completion-out shape is
still real and still needs extending — `ShroudClient.Chat` itself has to
grow a `messages`+`tools` request and a `tool_calls`-aware response, and
`internal/llm.Shroud.Generate`'s one-string signature has to grow into
whatever shape a tool-calling caller needs — but the platform side of this
gap is closed. `docs/team.md`/`docs/lab.md`'s "single-shot" language about
Shroud describes nanobots' own client code today, not a limit of the API
underneath it, and should not be read as a reason a future change here is
blocked on 1Claw.

**On the second API this entry considered (`api.1claw.co`'s
`/v1/agents/{id}/chat`) and ruled out**: also not quite the finding it
looked like. An API-key (agent) caller already skips `/chat/unlock` —
that gate is for human principals only — so the 403 this entry hit was
this account's own token type, not evidence the endpoint is unusable for
an unattended caller in general. Moot either way: that endpoint's tools
come from a runtime's own tool registry rather than ad hoc request tools,
and `/v1/chat/completions` is the right surface for `ai.generate` regardless.

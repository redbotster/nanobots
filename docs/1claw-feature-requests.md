# 1Claw feature requests

Things nanobots needs from 1Claw that do not exist yet, what each one
blocks, and the honest workaround shipped in the meantime.

Every entry was checked against `https://api.1claw.co/openapi.json` (499
paths) or a live call, not against the prose docs — the prose doc set
currently reports several shipped features as absent, so it is not a safe
source for "does this exist".

Format: **what we need** / **what it blocks** / **what we do instead**.

---

## 1. Connector presets for Stripe, HubSpot, LinkedIn, Drive, Google Business Profile

**What we need.** `GET /v1/connectors/presets` returns nine: `gmail`,
`google-sheets`, `google-calendar`, `github`, `slack`, `x`, `discord`,
`notion`, `honcho`. We need presets for Stripe, HubSpot, LinkedIn and Google
Drive, and for Google Business Profile we have no path at all.

**What it blocks.** `internal/stripe`, `internal/hubspot` and
`internal/linkedin` (608 lines) stay hand-rolled, each holding a credential
this repo would rather never see. Drive is the sharpest one: six bots use
it, so `internal/google` (1109 lines) cannot be retired even though Gmail,
Sheets and Calendar all have presets. `review-responder` declares Google
Business Profile and cannot run against a real account at all.

**What we do instead.** Keep the direct clients, with credentials in the
1Claw vault and read at call time by `nanobotd`, never inside a container.
Documented in the README's real/simulated table.

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

## 3. Cancelling or withdrawing an approval request

**What we need.** A way for the requester to withdraw a pending approval on
`/v1/approvals/*`. `POST /v1/pending-approvals/{id}/cancel` exists, but that
is a different resource family (platform-executed actions); there is no
equivalent under `/v1/approvals`.

**What it blocks.** Answering an approval in the nanobots UI leaves the
1Claw mirror pending forever. The person then gets a push notification for a
decision they already made, which trains people to ignore the notifications
that matter.

**What we do instead.** Leave the mirror pending and let it expire, and say
so in the log. Documented in `internal/runner/approver.go`.

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

**What we need.** Two things for `nanobots deploy 1claw`:

- A `nanobots` runtime template, or any template that runs a static binary.
  `GET /v1/runtimes/templates` returns nine and they are all language
  runtimes (python, node) or agent frameworks (hermes, openclaw,
  openclaude, opencode, claude-code, codex, amp). None runs a Go program,
  so a deploy needs the user to build and push their own image first.
- A file-transfer API for a runtime, so a swarm written locally can be
  pushed to a hosted one. There is `POST /v1/runtimes/{id}/shell/session`,
  but driving a shell to move files is not an interface to build on.

**What it blocks.** The hosted path being one command. Today
`nanobots deploy 1claw` requires `--image` and cannot carry the user's own
swarms; both are reported by the command rather than discovered later.

**What we do instead.** Ship the `Dockerfile` and tell people to push it.
`docs/hosting.md` says exactly what does and does not travel.

---

## 12. Memory search that matches something

**What we need.** `POST /v1/agents/{id}/memory/search` to return the entries
a query is about. It is in the spec, with a `top_k` and a score per result,
and it answers every request successfully — with nothing in it.

Probed against the live account, one entry in the namespace:

```
PUT    .../memory/probe-ns/obs-live-1  "refunds always get escalated"  -> stored
GET    .../memory/probe-ns             -> the entry, tier "durable"
search {"query":"refunds"}                          -> 0 results
search {"query":"refunds always get escalated"}     -> 0 results   (exact text)
search {"query":"escalated"}                        -> 0 results
search {"query":""}                                 -> 1 result
```

An empty query returns everything; any non-empty query returns nothing, the
stored text character for character included, and 60 seconds of waiting
changes neither. Nothing among the 499 paths configures an embedding model,
and the agent has `memory_enabled`.

**What it blocks.** Recall on the 1Claw memory backend. `inbox-triage`,
`support-triage` and `draft-replies` each ask a question in plain language
about what they have seen before; on 1Claw they get `ErrNoRecall` and
degrade, which is the honest outcome but not the useful one.

**What we do instead.** The 1Claw backend stays key/value and says so, so a
bot that needs recall fails loudly rather than being told "nothing known"
forever. Recall comes from Honcho instead (`docs/memory.md`). The code that
would wire this up was written and then removed rather than shipped dark:
see the comment on `memory.OneClaw`.

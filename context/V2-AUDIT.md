# V2 audit: what 1Claw already runs, and what nanobots should stop running

Written for the v2 plan. One row per `internal/` package: does 1Claw have a
native equivalent, is it strictly better, and should the package be
replaced, wrapped, or kept.

## How this was established

Two sources, in this order of trust:

1. **`https://api.1claw.co/openapi.json`** — 499 paths, fetched and read
   directly. This is the authority, and the plan's instruction to bind to
   real shapes rather than guess is the reason this audit is worth anything.
2. **`https://1claw.co/llms-full.txt`** — the prose doc set.

**They disagree, and the prose is wrong.** The doc set reports Automations,
Declarative Charts, OTel telemetry and three-tier memory as absent or
undocumented. The OpenAPI spec has `/v1/automations` (12 paths),
`/v1/otel/*` (7), `/v1/peers/*` (9, including `predict-approval`), and
`/v1/agents/{id}/memory/search`. This repo has been calling `/v1/otel/*`
successfully for weeks, which settles it. Where this audit and the prose
differ, the spec and live calls win, and the difference is noted.

Live calls made against the real account while writing this (read-only
except where stated): `/v1/connectors/presets`, `/v1/automations/step-types`,
`/v1/runtimes/templates`, `/v1/auth/agent-token`, `/v1/approvals/request`.

## The finding that changes the most

**An agent can open an approval. We had concluded it could not.**

`POST /v1/approvals/request` is marked agent-only and refuses the Human API
key with `403 "Only agents can request approvals."` Two days ago this repo
concluded the phone-approval path was blocked upstream, because an agent's
`ocv_` key is rejected on `api.1claw.co`. That conclusion was wrong, and the
missing step is one endpoint:

```
POST /v1/auth/agent-token   {"api_key": "ocv_..."}   -> {"access_token": "<JWT>", ...}
POST /v1/approvals/request  Authorization: Bearer <that JWT>
```

Verified end to end: the exchange returns 200 with a 1657-character JWT, and
the approval request then returns **202** with a real pending approval. The
first attempt returned `422 Missing required field: target_type`, which is
itself proof the credential was accepted.

`RunQueueApprover.mirror` already builds the right request body; it sends it
with the wrong client. This is the single highest-value item in the whole
plan and it is roughly a day of work, not a phase: it turns the largest
source of failed runs on this machine (54 runs, 27 hours of container time,
every one an approval nobody was awake to answer) into a solved problem,
with SMS/push/email delivery included.

It also invalidates the caveats currently written into `approver.go`,
`docs/oneclaw-bridge.md` and the `nobody answered` remedy. Those were
honest when written and are now wrong; Phase 2 item 12 must correct them.

## Per-package audit

Legend: **Replace** = delete ours, adapt to theirs. **Wrap** = keep our
interface, back it with theirs. **Keep** = no 1Claw equivalent, or ours is
better for a stated reason.

| Package | LOC | 1Claw equivalent | Verdict | Why |
|---|---:|---|---|---|
| `github` | 126 | Connector preset `github` | **Replace** | Preset exists. Credentials move server-side, calls land in the audit trail. |
| `slack` | 119 | Connector preset `slack` | **Replace** | Same. |
| `x` | 256 | Connector preset `x` | **Replace** | Same, and deletes a hand-rolled OAuth2+PKCE consumer. |
| `google` | 1109 | Presets `gmail`, `google-sheets`, `google-calendar` | **Wrap, partly** | Three of four covered. **No Drive preset** — Drive is used by six bots, so `internal/google` cannot go until that lands. |
| `stripe` | 115 | none | **Keep** | No preset. Feature request filed. |
| `hubspot` | 214 | none | **Keep** | No preset. Feature request filed. |
| `linkedin` | 279 | none | **Keep** | No preset. Feature request filed. |
| `oauth2pkce` | 425 | Connector install flow | **Replace** once x/linkedin move | Only exists to serve `x` and `linkedin`. |
| `memory` | 610 | `/v1/agents/{id}/memory/*` + `/memory/search` | **Wrap** | Semantic search is real (`top_k`), so `memory.recall` stops degrading. Keep the local backend for offline. |
| `scheduler` | 1103 | `/v1/automations` (`trigger_type: cron`) | **Wrap, do not replace** | See the 300s ceiling below. Keep local as the default. |
| `api` (webhooks) | — | `/v1/automations/webhook/{id}/{token}` | **Wrap** | Gains a rotatable `whk_` token and off-laptop firing. |
| `oneclaw` | 2278 | — | **Keep and grow** | This is the adapter. It gets bigger as everything else shrinks. |
| `runner` | 3497 | Cloud Runtimes (`/v1/runtimes`) | **Keep** | Runtimes host a process; they do not run our per-bot container model. Runtimes are a deployment target, not a replacement. |
| `planner` | 1610 | none | **Keep** | Typed-port checking is ours and is the product. |
| `step` | 4596 | Automation `workflow_spec` steps | **Keep** | See "the overlap that is not one" below. |
| `foundry` | 1534 | Runtime templates | **Keep** | Different job: authoring a bot vs hosting a process. |
| `llm` | 755 | Shroud | **Keep** | Already routes through Shroud; the other backends are the documented escape hatch. |
| `roles`, `share`, `schema`, `contract`, `remedy`, `wiring`, `daemon`, `service` | 3073 | none | **Keep** | No equivalent; these are the product's own shape. |

Deletable today: `github` + `slack` = **245 lines**, once the connector path
passes the fake-server tests those packages already have. Deletable after
one feature request each: `x` + `linkedin` + `oauth2pkce` = **960 lines**.

## Where the plan's premises need correcting

**Item 13 (triggers move to Automations) has a hard ceiling.** The `http`
step type reports, verbatim: *"Bounded by the 300s whole-run timeout rather
than a per-step one."* Seven of the sixteen catalog swarms pause for a human
approval, and a person is not reliably back inside five minutes. An
Automation cannot host those. The plan already anticipates this and asks for
a feature request; the audit's stronger recommendation is that the local
scheduler stays the **default** rather than the offline fallback, with
Automations as opt-in for swarms that do not wait on a human.

Also: the automation `http` step is `agent_allowed: false`. Automations
that call back into nanobotd must be created by a human credential, not by
the composer agent. That constrains how `nanobots publish` can work.

**Item 16 (Declarative Charts) has no API.** No `chart` path exists in 499.
Either it is CLI-only or it is not shipped. Do not build against it until
confirmed; feature request filed.

**Item 11 (three-tier memory) is two tiers, not three.** The spec has
key/value per namespace and a semantic `search`. There is no documented TTL
"scratch" tier. Map `memory.get/put` to KV and add `memory.search`; do not
promise a scratch tier in the README.

**Item 15 (agent quota) is the right call and now urgent.** This account is
at 25 of 50 agents, 24 of them nanobots-created, one per bot name. Since the
approval mirror needs an agent, the per-bot model would have made every
approving bot consume a slot. One agent per user, namespaced memory and
bindings per bot, is both cheaper and the thing that makes item 12 free.

## The overlap that is not one

1Claw's automation step vocabulary (`http`, `ai_generate`, `memory_get`,
`memory_put`, `memory_search`, `approval_request`, `notify`, `condition`,
`log`, …) looks like `internal/step`'s vocabulary, and the tempting
conclusion is that `internal/step` is redundant.

It is not, and this is worth stating before someone tries. Automations are a
linear workflow over a 300-second budget with untyped JSON between steps.
`internal/step` executes one bot's steps inside a sandbox, and the planner
type-checks the ports *between* bots before anything runs. The typed DAG is
the product. Delegating the platform underneath it is the plan; delegating
the composition layer would be deleting the thing people would choose this
over n8n for.

## Recommended order, against the plan's own numbering

The plan says do not reorder phases, and this audit does not. It does say
which item inside a phase to do first:

1. **Phase 2 item 12 (approvals) before anything else in Phase 2.** It is
   the biggest real-world win, it is nearly free now, and it corrects three
   documents that are currently wrong.
2. **Phase 2 item 15 (agent quota) immediately after**, because item 12
   makes agent count matter more.
3. Phase 1 remains first overall as written; item 3 (single binary) is what
   most users judge.

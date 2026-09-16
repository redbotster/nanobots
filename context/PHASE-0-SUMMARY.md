# Phase 0 summary: audit and the feature-request log

Stopping here for review, as the plan asks.

## What shipped

- `context/V2-AUDIT.md` — every `internal/` package against its 1Claw
  equivalent, with a replace / wrap / keep verdict and the evidence.
- `docs/1claw-feature-requests.md` — ten entries, each as *what we need* /
  *what it blocks* / *what we do instead*.
- One line in `README.md` linking the new doc, because
  `TestReadmeLinksEveryDoc` failed until it did. Fixed the claim, not the
  test.

No code changed. Lines added 0, removed 0.

## The finding that should change the plan's order

**An agent can open a 1Claw approval. This repo concluded two days ago that
it could not, and that conclusion was wrong.**

`POST /v1/approvals/request` is agent-only and refuses the Human API key
with `403 "Only agents can request approvals."` The earlier investigation
tried the agent's `ocv_` key as a bearer (401) and tried exchanging it at
the *human* endpoint `/v1/auth/api-key-token` (401), and stopped there. The
step it missed:

```
POST /v1/auth/agent-token   {"api_key":"ocv_..."}  -> 200, access_token (JWT)
POST /v1/approvals/request  Authorization: Bearer <JWT>
```

Verified end to end against the live account: the exchange returns a
1657-character JWT and the approval request then returns **202** with a real
pending approval. The first attempt returned `422 Missing required field:
target_type`, which is itself proof the credential was accepted.

Why it matters: the single largest source of failed runs on this machine is
scheduled swarms whose approval nobody was awake to answer — 54 runs, 27
hours of container time. Seven of the sixteen catalog swarms are in that
shape. This unblocks SMS, push and email delivery for those approvals.
`RunQueueApprover.mirror` already builds the correct request body; it sends
it with the wrong client.

Recommended: do **Phase 2 item 12 first**, then item 15 (agent quota, which
item 12 makes matter more). Phase 1 stays first overall.

## Where the plan's premises need correcting

Followed the evidence, as instructed.

1. **The prose docs are unreliable for "does this exist".**
   `https://1claw.co/llms-full.txt` reports Automations, Declarative Charts,
   OTel and tiered memory as absent or undocumented. The OpenAPI spec has
   `/v1/automations` (12 paths), `/v1/otel/*` (7), `/v1/peers/*` (9), and
   `/v1/agents/{id}/memory/search`. This repo has been calling `/v1/otel/*`
   for weeks. Everything below is from the spec or a live call.

2. **Item 13 has a hard ceiling.** The automation `http` step says, verbatim,
   *"Bounded by the 300s whole-run timeout rather than a per-step one."* A
   swarm that waits on a human cannot live in an Automation. The audit's
   recommendation is stronger than the plan's: the local scheduler stays the
   **default**, not the offline fallback. Also `http` is
   `agent_allowed: false`, so `nanobots publish` needs a human credential.

3. **Item 16 has no API.** No path matching `chart` exists among 499.
   Confirm it is CLI-only before building against it.

4. **Item 11 is two tiers, not three.** Key/value per namespace plus
   semantic `search` with `top_k`. No TTL scratch tier. The README should
   say two.

5. **Item 8 is partly blocked.** Presets exist for gmail, google-sheets,
   google-calendar, github, slack, x, discord, notion, honcho. **No Drive
   preset**, and six bots use Drive — so `internal/google` cannot retire on
   the schedule the plan implies. `internal/github` and `internal/slack`
   (245 lines) can go now.

6. **`internal/step` is not redundant.** The automation step vocabulary
   looks like ours, and the audit explains at length why delegating it would
   be deleting the product rather than the platform underneath it.

## Verification

Phase 0 changed no code. Ran the full pass anyway, since the README changed:

```
go build ./...          ok
go vet ./...            ok
gofmt -l .              clean
go test ./... -race     ok   (654 tests)
npx tsc -b              ok
npm run lint            0 problems
npm run format:check    clean
npm run test            88 passed
```

## Real vs. simulated

No movement this phase — nothing was built. The audit names what will move
and in which item; item 12 moves "approvals reach your phone" from a
documented gap to the left column.

## New feature requests

Ten, in `docs/1claw-feature-requests.md`. Four are new to this audit and
were not in the plan's seed list:

- **#8** Declarative Charts have no API endpoint at all.
- **#9** Memory has two tiers, not three.
- **#10** An agent cannot list the approvals it created (`GET /v1/approvals`
  is human-only), so a crash between creating a mirror and recording its id
  orphans it.
- Drive added to **#1**, which the plan's seed list did not mention and
  which is the one actually blocking `internal/google`.

## One thing to clean up, mine

Proving the approval path works created **two real pending approvals** on
the 1Claw account, both summarised *"nanobots probe: can an agent open an
approval?"*:

```
82434632-1254-452d-a8b0-284c5db1b1cd
d0f113ae-cac5-44fe-bbf5-17e22319de26
```

I could not resolve them: `POST /v1/approvals/{id}/decide` is human-only and
refuses an agent with `403 "Only human users can decide approvals."`, which
is correct behaviour. They need rejecting from the 1Claw dashboard. Worth
noting that this is feature request #3 (no withdraw) biting immediately.

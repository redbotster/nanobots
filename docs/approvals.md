# Approvals

Every bot whose job includes send/post/pay/delete gets an `approve` step before the side effect — that's a catalog-wide rule (`NANOBOTS-CATALOG.md`), not a per-bot choice. `email-drive-file`'s `gate` step is this build's example: it pauses before `messages.send`, no matter what.

## One mechanism, not two

An `approve` step's `Deps.Approve(summary, riskTier)` call is routed to `internal/runner.RunQueueApprover`, regardless of whether the bot behind it is running against real 1Claw (`LiveDeps`) or fixtures (`DemoDeps`) — both accept an `Approver` override for exactly this reason (see `internal/step/live.go`, `internal/step/demo.go`). A run-in-progress looks the same to a human either way: the run's status flips to `awaiting_approval`, a `PendingApproval` appears with the exact summary text, and everything blocks until a decision arrives — from the WebUI (`POST /api/runs/{id}/approvals/{approvalId}/decide`) or the CLI (`nanobots run` prompts on the terminal directly).

This happens live, not after the fact: `RunQueueApprover.Approve` calls `Run.RequestApproval`, which logs `"awaiting approval: <summary>"` and flips the run's status *before* blocking — so a client watching the run's SSE stream or polling `GET /api/runs/{id}` sees the gate the instant it opens, while the bot's container sits blocked on the callback waiting for exactly that response.

## Try it

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

Watch for the terminal prompt right before the mail would send — that's the same gate the WebUI shows inline in the run log, with an Approve/Skip button instead of a `[y/N]`.

## Answering somewhere other than this tab

When 1Claw is configured, the same question is opened in its approval queue
as well as this one. Whichever answers first wins; the other is ignored, and
the run records who decided — "1claw (approved)" reads differently from
"cli" when you come back to it.

This closes a real hole in the scheduler story. A swarm on a cron trigger
fires at 08:00 and blocks on an approval; if the only way to answer is a
browser tab that happens to be open, an overnight run waits until someone
sits down. On this machine that cost 54 runs and 27 hours of container time
before it worked.

### How it asks

`POST /v1/approvals/request` is **agent-only**: sent with your Human key it
answers `403 "Only agents can request approvals."` So the mirror
authenticates as an agent, which needs its own exchange:

```
POST /v1/auth/agent-token   {"api_key":"ocv_…"}         -> a JWT
POST /v1/approvals/request  Authorization: Bearer <JWT> -> the approval
GET  /v1/approvals/{id}/status                          -> pending|approved|rejected
```

One agent does this for every approval, named `nanobots`
(`runner.ApprovalAgentName`), created on the first gate that opens and never
again. Not one per bot: agents are plan-capped, and a question addressed to
you is no clearer for being asked by `nanobots-email-send-approved`. It is
also the agent `nanobots deploy 1claw` runs as.

That agent is deliberately minimal — no Shroud budget, no memory, no vault,
no execution intents. It exists to ask a question and read the answer.

Deliberate details:

- **The local queue is the one that must work.** Every failure in the mirror
  is logged and dropped — a 1Claw outage costs you the convenience of
  approving from a phone, not the ability to approve at all. The run says so
  ("answerable here only") rather than failing quietly.
- **The poll stops when the question is answered here**, rather than running
  for the full thirty-minute timeout against a decision already made.
- **A local answer leaves the 1Claw approval pending.** Its API has no
  cancel, and a stale question is more honest than pretending to have
  withdrawn one (`docs/1claw-feature-requests.md` #3).
- **A fanned-out batch mirrors once**, like the local gate: one question
  naming the count, not twenty.
- **The poll is by id, not by listing.** `GET /v1/approvals` is human-only
  and refuses the agent, so an approval whose id is lost is unreachable to
  us (`docs/1claw-feature-requests.md` #10). The id goes in the run log the
  moment it is created.
- **Where the question actually appears is 1Claw's call.** This opens an
  approval in your 1Claw queue; whether that reaches a push notification, an
  email or only the dashboard is a setting on your account, not something
  this repo controls or should claim.

## The prompt says what "yes" does

It used to read:

```
Approval needed   Re: Export is failing on large workspaces   [Skip] [Approve]
```

which tells you what the thing is *about* and nothing about what approving
*does*. It now reads:

```
Approval needed  [MEDIUM RISK]  Invoice INV-1042 is 14 days overdue ($4,200)
sender will write to gmail. Declining stops the run here.
                                        [ Don't approve ]  [ Approve ]
```

Three things were already known and thrown away. The risk tier is on the
`approve` step. The bot's name is on the pending approval. And
`guardrails.writes_allowed` — the same field the share bundle uses for
"WHEN RUN, THIS SWARM CAN WRITE TO" — says exactly what the blast radius of
"yes" is. `PendingApproval.writes` now carries it.

"Skip" became "Don't approve". Declining does not skip a step and carry on;
it ends the run. A label that reads like *later* on a button that means
*no* is the wrong kind of gentle.

## Two things the prompt used to get wrong

Both found by running the catalog's own `approve` brick and reading what it
printed, which no test had ever done.

**The risk tier was the template, not the tier.** `bots/approve` declares
`risk_tier: "{{inputs.risk}}"`, and the interpreter resolved the summary but
passed the tier straight through. Every gate that brick opened asked you to
approve something at `{{inputs.risk}}` risk — printed verbatim in the CLI and
in the WebUI's risk badge. Quieter and worse: the unresolved string is not
`low`, `medium` or `high`, and 1Claw's `declared_risk_tier` mapping sends
anything it does not recognise to the strictest tier, so every one of them
was declared a tier-1 decision.

**An answered question kept being asked.** The goroutine waiting on a gate
is what removes it from the run's pending list, so for a moment after you
answer, the approval is still listed. The CLI re-checks that list on every
log line — and one line arriving in that window (the 1Claw mirror's, as it
happens) was enough to prompt a second time, hit end-of-input on a drained
stdin, and print:

```
declined — nobody — no terminal attached to ask
```

on a run that had just been approved. That second decision also set the
run's `DeclinedByUser` flag, and would have blocked on the run's own mutex,
holding it, if the waiter had not already drained the one-slot channel.
`Decide` now refuses a second answer, and `PendingApprovals` stops listing a
question the moment it is answered rather than when the waiter wakes.

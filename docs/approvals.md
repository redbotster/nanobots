# Approvals

Every bot whose job includes send/post/pay/delete gets an `approve` step before the side effect — that's a catalog-wide rule (`NANOBOTS-CATALOG.md`), not a per-bot choice. `email-drive-file`'s `gate` step is this build's example: it pauses before `messages.send`, no matter what.

## One mechanism, not two

An `approve` step's `Deps.Approve(summary, riskTier)` call is routed to `internal/runner.RunQueueApprover`, regardless of whether the bot behind it is running against real 1Claw (`LiveDeps`) or fixtures (`DemoDeps`) — both accept an `Approver` override for exactly this reason (see `internal/step/live.go`, `internal/step/demo.go`). A run-in-progress looks the same to a human either way: the run's status flips to `awaiting_approval`, a `PendingApproval` appears with the exact summary text, and everything blocks until a decision arrives — from the WebUI (`POST /api/runs/{id}/approvals/{approvalId}/decide`) or the CLI (`nanobots run` prompts on the terminal directly).

This happens live, not after the fact: `RunQueueApprover.Approve` calls `Run.RequestApproval`, which logs `"awaiting approval: <summary>"` and flips the run's status *before* blocking — so a client watching the run's SSE stream or polling `GET /api/runs/{id}` sees the gate the instant it opens, while the bot's container sits blocked on the callback waiting for exactly that response.

## Not yet done

1Claw has its own approval system (`POST /v1/approvals/request`, visible in the dashboard and mobile app) — `internal/oneclaw.RequestApproval`/`WaitForApproval` are real, tested clients for it, and `LiveDeps.Approve` uses them directly when no `Approver` override is set. The local runner doesn't mirror a live bot's approvals into that system today; every approval in a `nanobots run`/WebUI run stays local to that run's own queue. See the TODO in `internal/runner/approver.go`.

## Try it

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

Watch for the terminal prompt right before the mail would send — that's the same gate the WebUI shows inline in the run log, with an Approve/Skip button instead of a `[y/N]`.

## Answering from your phone

When 1Claw is configured, the same question is opened in its approval queue
as well as this one. Whichever answers first wins; the other is ignored, and
the run records who decided — "1claw (approved)" reads differently from
"cli" when you come back to it.

This closes a real hole in the scheduler story. A swarm on a cron trigger
fires at 08:00 and blocks on an approval; if the only way to answer is a
browser tab that happens to be open, an overnight run waits until someone
sits down. Now it doesn't.

Deliberate details:

- **The local queue is the one that must work.** Every failure in the mirror
  is logged and dropped — a 1Claw outage costs you the convenience of
  approving from a phone, not the ability to approve at all. The run says so
  ("answerable here only") rather than failing quietly.
- **The poll stops when the question is answered here**, rather than running
  for the full thirty-minute timeout against a decision already made.
- **A local answer leaves the 1Claw approval pending.** Its API has no
  cancel, and a stale question is more honest than pretending to have
  withdrawn one.
- **A fanned-out batch mirrors once**, like the local gate: one question
  naming the count, not twenty.

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

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

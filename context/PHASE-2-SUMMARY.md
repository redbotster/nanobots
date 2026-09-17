# Phase 2 summary: what 1Claw actually does

Stopping here for review, as the plan asks.

Phase 2 was "use 1Claw for what 1Claw is for". Four of its items had a claim
about the platform underneath them, and the honest result is that **three of
those claims were wrong** — two in our favour, one against.

## What shipped

| Item | Commit | Outcome |
|---|---|---|
| 12. Approvals through 1Claw | `d15f52c` | Built. An overnight swarm's gate can be answered away from the tab that started it. |
| 15. Agent quota | `8601648` | Built, differently to the plan: one agent per guardrail profile, 27 to 2. Plus `nanobots agents [--prune]`. |
| 11. Memory tiers | `1ffa6be` | Not built, deliberately. The search endpoint answers every request with nothing in it. |
| 13. Triggers to Automations | — | Not built. An Automation cannot reach a laptop, and the useful case is blocked upstream. |
| 16. Declarative Charts | — | No API exists. Filed as feature request #8 in Phase 0; nothing to build against. |

Plus two fixes the work exposed: an approval prompt that showed
`{{inputs.risk}}` instead of the tier and asked the same question twice
(`645afce`), and a test of mine that raced (`6854de3`).

## Item 12 — the conclusion that was written down as fact

`POST /v1/approvals/request` is agent-only. This repo had probed it, been
refused twice, and recorded "approvals cannot reach 1Claw" in four places:
the approver's own comment, `needsOneClawAgent`, `docs/oneclaw-bridge.md`,
and the "nobody answered" remedy.

The two refusals were real. The conclusion was not:

```
human key    -> 403 "Only agents can request approvals."     (true, still true)
agent ocv_   -> 401 "Invalid or expired token"                (true, and not the point)
```

An agent key has its own exchange, `POST /v1/auth/agent-token`, distinct
from the human `/v1/auth/api-key-token`. Two refusals were read as
*impossible* when they meant *not like that*.

Cost of the gap: 54 runs on this machine died on an approval nobody was
awake to answer, 27 hours of container time, and across 108 runs that opened
a gate not one logged either mirror outcome.

One agent opens every approval, named `nanobots` — not one per approving
bot. The fake 1Claw in the tests now enforces the real rule, so a mirror
built on the human client fails the suite instead of passing it.

## Item 15 — the audit's suggestion was cheaper and wrong

The audit said one agent per user. An agent *is* the guardrail boundary:
`shroud_config` — PII policy, injection threshold, allowed providers, daily
budget — is set per agent, and `SendChatMessageRequest` has no per-call
override. One agent means one policy for every bot.

Agents are keyed by guardrail profile instead. Measured against the real
catalog first: the 27 bots that need an agent declare exactly two profiles,
sixteen `pii: redact` and eleven `pii: allow`. So 27 agents to 2, and a bot
that declares something different still gets its own automatically.

`nanobots agents` is the other half, because renaming the scheme tidies
nothing by itself. Its first version would have deleted the live
`nanobots-composer` and `nanobots-shroud-proxy` agents, whose api_keys are
shown exactly once — now `internal/agentname` is the single list, read both
by the code that creates agents and by the code that decides what is stale.

## Item 11 — a route that answers is not a route that works

`POST /v1/agents/{id}/memory/search` is in the spec with a `top_k` and a
score per result. The prose docs say it does not exist. The spec is normally
the one to trust, so it was probed with one entry in the namespace:

```
search {"query":"refunds"}                          -> 0 results
search {"query":"refunds always get escalated"}     -> 0 results   (the exact text)
search {"query":""}                                 -> 1 result
```

Empty query returns everything, any real query returns nothing, 60 seconds
of waiting changes neither, and nothing among the 499 paths configures an
embedding model.

The `Recaller` implementation was written and then removed rather than
shipped dark. A recall that always answers "nothing known" is worse than
`ErrNoRecall`, because a bot cannot tell it from an empty memory.

The same probe found our **own** feature request #9 was wrong: the TTL tier
exists (`ttl_seconds` in, `ttl_expires_at` back, verified). Nothing uses it —
no bot has scratch state that should expire — so it is recorded, not built.

## Item 13 — why the local scheduler stays the default

Three measurements, all from `GET /v1/automations/step-types`:

- **An Automation cannot reach your machine.** Its only path back into
  nanobots is the `http` step, described as SSRF-validated — which is
  exactly what refuses `http://127.0.0.1:7474`. Automations can drive a
  publicly reachable deployment, not a laptop.
- **`http` is bounded by the 300s whole-run timeout.** Seven of the sixteen
  catalog swarms pause for a human. Five minutes is not how long someone
  takes to answer; the local gate allows thirty.
- **`http` is `agent_allowed: false`**, so an Automation that calls back
  into nanobots must be created with a human credential — the composer
  cannot propose one that works.

What remains is real but narrow: a hosted deployment, running swarms that
never wait on a person. That needs an image you have pushed
(`docs/1claw-feature-requests.md` #11), so building the command now would
ship something whose first step is missing. Written up in
`docs/scheduler.md` rather than half-built.

## Two things for a human

1. **Verifying item 13 properly means creating an Automation** on the
   account — and then deleting it, which needs permission. The analysis
   above is from the step-type descriptions and the spec, not from a live
   Automation.
2. **24 leftover agents** from the old per-bot scheme are still on the
   account. `nanobots agents --prune` will delete them after listing every
   name and asking. It has not been run.

## Recommended next

Phase 3, or the largest gap between what this repo says and what a stranger
can do: **there is still no tagged release**, so `brew install` and
`npx nanobots` do not work and `nanobots deploy 1claw` needs an image the
user builds themselves. That one prerequisite is also what unblocks item 13.

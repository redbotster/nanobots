# Working on nanobots

## What we optimise for

Every change should move at least one of these, and never quietly trade one
away without saying so:

- **Easier to use** — fewer steps to the thing you wanted, and the common
  case works without reading anything.
- **Clearer** — the app never claims something that isn't true, and a
  failure says what to do about it.
- **More powerful** — the thing you can obviously want, you can do.
- **More compact and optimised** — less code, fewer bytes, less waiting, for
  the same or more behaviour.
- **More useful** — it does real work on real accounts, not a demo of doing
  work.
- **More modular** — one place per fact, so two copies cannot drift apart.

## The rule that catches the most bugs

**Never let the app say something that isn't so.** Almost every real defect
found in this codebase has been an honesty bug, not a crash:

- a composed swarm described as "Every Friday" with `trigger: {type: manual}`
- `0/4 live` on all fifteen cards, reading as "everything is broken" when it
  meant "nothing is connected yet"
- `FAILED: stopped from the app` for a run the user deliberately stopped
- "stopping the container failed — it may still be running" when it was gone
- a masked token printed in full in the `curl` line two rows below
- a schedule that had failed 85 times, still queueing, with nothing saying so

When a label, status, count or error could be read as a claim, check the
claim is true in the case that actually happens.

## Measure before improving

Look at the running system, not at the code's intentions. Things found only
by measuring:

- 198 runs, 135 failed, 85 of them one swarm repeating one failure
- `GET /api/runs` at 91KB polled every 2s, near-always byte-identical
- the bot library at 6.8 screens with 4 of 7 headings holding one bot
- 31 unnamed switches, all of them account-connecting controls

`curl` the endpoint, count the rows, time the poll, screenshot the page.

## Verify in a real browser

Unit tests pass on things that are visibly broken. A browser pass has caught
what green tests did not: a nested `<button>`, a token masked in one place
and printed in another, a panel squeezed into a header's flex slot. Drive
the UI, take the screenshot, look at it.

When measuring in a browser, note that React batches — reading `el.style`
right after dispatching an event shows the *old* value. Split the dispatch
and the read.

## Verification (all of it, every time)

```sh
go build ./... && go vet ./... && go test ./... -race
cd web && npx tsc -b && npm run test
```

Some claims check themselves, in `internal/contract` — the README's test
count, bot count and swarm count; that every doc is linked from the README;
that `docs/bot-contract.md` names every step type; that `schemas/*.json`
match the Go types. If you add a doc or a test, they will tell you. Fix the
claim, don't weaken the test.

## Comments say why, with the evidence

The house style is a comment that records the incident, not the mechanism:

```go
// "No such container" and "is not running" are successes, not failures.
// Containers run with --rm, so cancelling races their own cleanup: the
// common case for a stop is that the container is already gone by the
// time we ask. Treating a non-zero exit as failure made a clean stop
// report "stopping the container failed — it may still be running as
// nanobot-…", which was both alarming and false.
```

If you cannot name what went wrong, the comment probably isn't needed.

## Don't delete a field you don't model

`swarmmerge.go` exists because "Save changes" once destroyed a swarm's cron
trigger and its guardrails: the builder round-trips a swarm, and a field it
doesn't know about is a field it deletes. Editing merges into the existing
YAML rather than replacing it.

Anything optional that can also be *deliberately cleared* needs three
states, not two — `schedule` is `*string`: absent means leave it alone, `""`
means make it manual.

## Report honestly

Say what was done, what was measured, and what was left. If a change is
cosmetic rather than a saving, say so — removing the empty panels from the
bot library made it look right without making the page one pixel shorter,
and the commit says that.

## Repo shape

- `cmd/` — three binaries: `nanobots` (CLI), `nanobotd` (daemon),
  `nanobot-agent` (runs inside each container). Nothing else belongs here.
- `internal/` — `step` (the universal step interpreter), `runner`
  (orchestration, Docker, runs), `api` (REST+SSE), `planner`, `scheduler`,
  `schema`, `foundry`, plus the pluggable `llm` and `memory` backends.
- `bots/` — 39 catalog bots, one directory each.
- `examples/swarms/` — 16 swarms: 12 cron, 2 event, 1 webhook, 1 manual.
- `web/` — the React app. `docs/` — 21 pages, all linked from the README.

Credentials live in `~/.secrets/nanobots.env` and never in the repo or a
log. Never delete a 1Claw resource without asking first.

# Running a swarm's independent branches at once

A swarm is a DAG, but it used to run as a list — then, for a while, as a
list of stages ("waves"): the planner grouped the DAG into levels where
every bot in a level had all its dependencies satisfied by earlier levels,
and the runner ran a level's bots concurrently but waited for the whole
level to finish before starting the next.

**That's gone.** The runner now starts each bot the instant its own
upstream bots have finished — nothing else. Two bots with no path between
them in the DAG can start, run, and finish in either order or at the same
time, whether or not a topological sort happened to put them in the same
numbered stage. `internal/planner.DAG.Levels()` still exists — it's a
legitimate way to *describe* a swarm's structure (the table below is
generated from it, and `nanobots plan`'s report can show it) — but the
runner no longer asks it anything. See `internal/runner.runDAG`'s own doc
comment for the mechanism: every bot gets a goroutine immediately, and each
one's first act is waiting on the specific bots it actually reads from.

Measured directly: `internal/runner.runDAG` run against the real, planned
DAG of two catalog swarms, with each bot's real work replaced by a fixed
sleep (no Docker, no model calls — this isolates the scheduler from
everything else that makes a run's timing noisy) and the old wave-boundary
number computed from the same per-bot durations against `Levels()`'s own
grouping, for a same-inputs comparison:

- `morning-brief`: `triage` feeds both `brief` and `notifier`; `meetings`
  feeds nothing downstream at all. With `meetings` taking 900ms and
  everything else 300ms, the old scheduler's wave boundary held `brief`
  and `notifier` back until `meetings` finished even though neither reads
  anything it produces: 1.2s. `runDAG` starts them the instant `triage`
  is done: 902ms.
- `bookkeeping-assistant`: `receipts` feeds nothing downstream; the other
  four bots are a straight chain. With `receipts` at 900ms and the chain
  at 300ms each, the old scheduler's first wave grouped `receipts` with
  `reporter` and made the whole run wait for the slower of the two before
  starting `pdf`: 1.8s. `runDAG` starts `pdf` the instant `reporter` is
  done, regardless of `receipts`: 1.2s.

Both swarms happen to have a wave-1 bot with no downstream reader at all
(`meetings`, `receipts`) — the shape that makes a wave boundary purely a
cost, not a correctness requirement: nothing was ever waiting on the slow
bot, the old scheduler just assumed something might be because they
started in the same wave. A swarm where every bot in a wave really does
feed the next one gains nothing from this change, because there was
nothing to gain — the wave boundary and the real dependency already
lined up.

## What actually benefits

Five of the eighteen catalog swarms have a wave wider than one — "wave"
here names `Levels()`'s own grouping, the same one this page used to
describe the runner's schedule with; it's still the right way to answer
"does this swarm have any branching at all," which is what the table
below actually shows:

| swarm | bots | waves |
|---|---|---|
| `morning-brief` | 4 | 2, 2 |
| `bookkeeping-assistant` | 5 | 2, 1, 1, 1 |
| `lead-to-meeting` | 5 | 1, 1, 2, 1 |
| `meeting-to-action` | 4 | 1, 1, 2 |
| `support-desk-lite` | 3 | 1, 2 |

The other thirteen are straight chains and gain nothing from *this*. Worth
saying plainly: it is not a general speed-up, it is a speed-up for swarms
that branch — and, as of this phase, a speed-up that no longer needs the
two branches to happen to be the same size to fully realize.

## The other kind of branch: a fanned-out bot

A fan-out is one bot run once per item, and the items are independent by
definition — that is what fanning out means. They ran one after another
anyway, which is a different sequential loop from the one above and was
missed when that one was fixed.

Found by reading one real run rather than the code. `supervisor-review`, 50
seconds, every single second of it an `ai.generate` call:

```
  +   0.0s  (+  0.0s)  board   starting
  +   9.1s  (+  9.1s)  board   ai.generate ./prompts/choose.md -> ok
  +   9.1s  (+  0.0s)  panel   running once per item — 3 from board.roles.*.name
  +  21.6s  (+ 12.5s)  panel   ai.generate ./prompts/review.md -> ok     item 1
  +  28.7s  (+  7.1s)  panel   ai.generate ./prompts/review.md -> ok     item 2
  +  35.5s  (+  6.9s)  panel   ai.generate ./prompts/review.md -> ok     item 3
  +  50.1s  (+ 14.5s)  chair   ai.generate ./prompts/synthesise.md -> ok
```

Three reviewers reading the same work, 26.5 seconds one at a time. Nothing
about the second review depended on the first.

The items now run under the same `NANOBOTS_MAX_PARALLEL_BOTS` cap the
cross-bot scheduler uses. Measured on the live
swarm, three pairs of runs alternating the two settings:

| | limit 1 | limit 4 |
|---|---|---|
| | 52.9s | 37.6s |
| | 41.4s | 34.0s |
| | 53.2s | 39.9s |

Model latency varies a lot run to run, which is why it is three pairs and
not one — but the direction is the same every time, around a quarter off.

Six of the eighteen catalog swarms fan out: `inbox-autopilot`, `get-paid`,
`support-desk-lite`, `supervisor-review`, `thread-from-an-idea`,
`never-drop-a-thread`.

## Why it's safe

A fanned-out bot's items were already isolated from each other before they
ran at once — each has had its own `item-N` workspace since fan-out was
written, and container names are UUIDs. The one thing that was *not* safe is
why `runBotOnce` returns its outputs now rather than stashing them under the
bot's name: twenty items writing to one slot and the loop reading it back
between them is a race that pairs the wrong output with the wrong item,
silently and only sometimes.

A failure stops the items that have not begun. The ones already in flight
finish rather than being cancelled — one mid-container would leave that
container orphaned, the same reason a fatal failure never cancels a bot
already running elsewhere in the swarm — so a failed fan-out can do up to
`limit-1` more items than the sequential version would have. The reported
error is the lowest-numbered failure, not whichever lost the race, so the
message is the one the sequential version produced.

A bot only ever waits on the specific upstream bots a `snap:` names — not
on "everyone the planner happened to group with it." That edge list is the
same one the planner type-checks snaps against, so there's one place, not
two, that decides who feeds whom. `TestLevelsFlattenToAValidRunOrderForEveryCatalogSwarm`
still asserts the older, equivalent property against `Levels()`'s own
grouping (no edge exists *within* one of its waves) — worth keeping as an
independent check on the planner's DAG construction, even though the
runner no longer consults `Levels()` itself.

Everything bots running at the same time share was already synchronised:
the run's log and outputs behind its mutex, the memory store behind its
own, the callback registry behind its.

## Failure

A bot that's already running, or one that's already past its dependency
wait and about to start, is never cancelled by a fatal failure elsewhere —
for two reasons, both true before this phase's rewrite and still true now.

A bot mid-container would leave that container orphaned — a leak this
runner has had once already. And when two independent bots fail, both
errors matter: independent branches usually fail for one shared reason, and
reporting whichever lost the race sends you looking for two bugs.

This is not hypothetical. The first real parallel run of `support-desk-lite`
reported two failures at once — a missing Slack credential *and* a fan-out
length mismatch that had been latent behind it. Run strictly sequentially,
only the first would ever have been visible. See docs/fan-out.md for the
second one.

A single failure reads exactly as it always did; only two or more get the
combined form. A bot genuinely downstream of a fatal failure — connected to
it by a real `snap:`, not just scheduled nearby — is still skipped, the
same as it always was: a fatal failure marks its own bot the same "gone"
a tolerated failure or a quiet watch already does, so anything reading from
it sees the same missing-upstream signal either way.

## Tuning

```
NANOBOTS_MAX_PARALLEL_BOTS=4    # default
NANOBOTS_MAX_PARALLEL_BOTS=1    # the old strictly-sequential behaviour
```

One cap, both kinds of branch. At 1, a fan-out is exactly the loop it
replaced: one item at a time, in index order. Above 1 it guarantees item `i`
is not launched until `i+1-limit` items have finished, so a run log stays
roughly in order — but not that item `i`'s first instruction runs before
item `i+1`'s. Once two goroutines are live the scheduler decides, and a test
claiming otherwise failed on its first run.

The cap was written when every bot was a container plus a model call; most
of the catalog runs in-process now (`docs/harnesses.md`), so four bots
running at once usually means four goroutines and four model calls, not
four container starts. The concern that motivated it hasn't disappeared,
just narrowed: several bots starting close together, or a fan-out, that happens to include several of the
few bots that still open a real Chromium (`meeting-prep`, `quote-builder`,
`recap-emails-to-pdf`, `render-pdf`, `sheet-reporter`) can still want a
couple of gigabytes at once, and a laptop that starts swapping finishes
slower than it would have sequentially. Set it to 1 on a small machine, or
when reading an interleaved run log is harder than waiting.

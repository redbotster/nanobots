# Running a swarm's independent branches at once

A swarm is a DAG, but it used to run as a list. The planner produced one
valid topological order and the runner walked it a bot at a time, so
`morning-brief` — four bots whose two halves never touch — ran as four
sequential container starts and four sequential rounds of model calls.

`DAG.Levels()` keeps the structure that `TopoSort` flattens away: waves,
where every bot in a wave has all its dependencies satisfied by earlier
waves. The runner runs a wave's bots concurrently and waits for the wave
before starting the next.

## What actually benefits

Five of the eighteen catalog swarms have a wave wider than one:

| swarm | bots | waves |
|---|---|---|
| `morning-brief` | 4 | 2, 2 |
| `bookkeeping-assistant` | 5 | 2, 1, 1, 1 |
| `lead-to-meeting` | 5 | 1, 1, 2, 1 |
| `meeting-to-action` | 4 | 1, 1, 2 |
| `support-desk-lite` | 3 | 1, 2 |

The other thirteen are straight chains and gain nothing from *this*. Worth
saying plainly: it is not a general speed-up, it is a speed-up for swarms
that branch.

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

The items now run under the same cap a wave uses. Measured on the live
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
container orphaned, the same reason a wave is allowed to finish — so a
failed fan-out can do up to `limit-1` more items than the sequential version
would have. The reported error is the lowest-numbered failure, not whichever
lost the race, so the message is the one the sequential version produced.

Bots in one wave have no path between them in the DAG, so nothing one
produces can be read by another. That's the property that makes this
correct rather than merely faster, and it's asserted directly — for every
swarm in the repo, `TestLevelsFlattenToAValidRunOrderForEveryCatalogSwarm`
checks that no edge exists *within* a wave, not just that the flattened
order is topologically valid.

Everything a wave's bots do share was already synchronised: the run's log
and outputs behind its mutex, the memory store behind its own, the callback
registry behind its.

## Failure

A failing wave is allowed to finish rather than being cancelled half-way,
for two reasons.

A bot mid-container would leave that container orphaned — a leak this runner
has had once already. And when two bots in a wave fail, both errors matter:
independent branches usually fail for one shared reason, and reporting
whichever lost the race sends you looking for two bugs.

This is not hypothetical. The first real parallel run of `support-desk-lite`
reported two failures at once — a missing Slack credential *and* a fan-out
length mismatch that had been latent behind it. Sequentially, only the first
would ever have been visible. See docs/fan-out.md for the second one.

A single failure reads exactly as it always did; only two or more get the
combined form. Later waves don't run after a failure.

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
just narrowed: a wave or a fan-out that happens to include several of the
few bots that still open a real Chromium (`meeting-prep`, `quote-builder`,
`recap-emails-to-pdf`, `render-pdf`, `sheet-reporter`) can still want a
couple of gigabytes at once, and a laptop that starts swapping finishes
slower than it would have sequentially. Set it to 1 on a small machine, or
when reading an interleaved run log is harder than waiting.

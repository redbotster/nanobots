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

Six of the fifteen catalog swarms have a wave wider than one:

| swarm | bots | waves |
|---|---|---|
| `morning-brief` | 4 | 2, 2 |
| `bookkeeping-assistant` | 5 | 2, 1, 1, 1 |
| `lead-to-meeting` | 5 | 1, 1, 2, 1 |
| `meeting-to-action` | 4 | 1, 1, 2 |
| `support-desk-lite` | 3 | 1, 2 |

The other nine are straight chains and gain nothing. Worth saying plainly:
this is not a general speed-up, it is a speed-up for swarms that branch.

## Why it's safe

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

The cap is about the machine, not the model: each bot is a container plus a
model call, four Chromium-bearing containers already want a couple of
gigabytes, and a laptop that starts swapping finishes slower than it would
have sequentially. Set it to 1 on a small machine, or when reading an
interleaved run log is harder than waiting.

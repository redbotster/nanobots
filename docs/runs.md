# Runs

## Stopping a run

Until this existed there was no way to stop anything. A bot that hangs holds
its container for the whole `max_runtime` ceiling — up to thirty minutes —
and watching was the only available action. On one real machine that ceiling
was hit 41 times by a single swarm.

A running run has a **Stop** button; `POST /api/runs/{id}/cancel` is the
same thing from a script. It answers 409 for a run that has already
finished, because "stopped" and "it was already over" are different
outcomes.

Three things had to be true for it to actually work:

- **Every run carries a cancellable context**, created with the run rather
  than taken from the HTTP request that started it. A run outlives that
  request; tying it there would end the run when the browser navigated away.
- **Killing the docker CLI does not kill the container.** The timeout path
  already knew this — verified against Docker 29.2.1, the container is still
  Up seconds after the client dies — so cancelling goes down the same road:
  `docker kill` by name, on a fresh context, because the run's is already
  cancelled by then.
- **An approval gate is not waiting on a context.** It is waiting on a
  person. Stopping a run parked at "Approval needed" has to release those
  explicitly, or the one thing still alive sits there for the rest of its
  window.

`docker kill` exiting non-zero with "no such container" is a *success*.
Containers run with `--rm`, so cancelling races their own cleanup and the
common case is that the container is already gone. Treating that as failure
reported "stopping the container failed — it may still be running as
nanobot-…", which was alarming and false; caught against a real stopped run
where `docker ps` showed nothing.

### It is not called a failure

The status stays `failed` — the run did not finish, and inventing a sixth
`RunStatus` would mean auditing every switch, tone map and terminal check
for a distinction the error already carries. But `stopped_by_user` is
reported alongside it, and everything that reads a run uses it:

- the run says **stopped** with a muted dot, not **failed** in red
- the per-bot log line reads `stopped`, not `FAILED: stopped from the app`
- the **Failed** filter on the Runs page does not sweep it up
- the scheduler's circuit breaker skips it entirely — five runs you stopped
  by hand are not a swarm that is broken, and pausing its schedule over them
  would be the app misreading a deliberate act

## A run that found nothing to do says so

A watch bot on a schedule produces a run every time it fires, and most of
those runs correctly do nothing: `drive-watch` looks at the folder, sees the
same file as last time, and stops ([bot-contract.md](bot-contract.md)).

Those runs **succeed**, because that is what happened. But an hourly watch
writes twenty-four of them a day, and a list where all twenty-four say
"succeeded" hides the one that acted — the same problem a wall of identical
red failures had, where eighty-five rows buried the two that were different.

So a run whose bots all either stopped or were skipped behind one carries
`nothing_to_do` with the reason, and the Runs page renders it as a muted dot
reading *nothing to do*, with the reason underneath and consecutive ones
collapsed into "and 23 more with nothing to do".

It is deliberately not a status. The run is `succeeded`; the scheduler's
circuit breaker sees a success, because a watch finding nothing is the
system working. Making it a fourth status would have meant every consumer —
history, the breaker, the filters, the API — learning a new word for
"fine".

## Polling costs almost nothing now

`GET /api/runs` is polled every two seconds by the Runs page and the
approval notifier (through one shared loop). On a machine with 199 runs that
response is 91KB and byte-identical between polls almost every time: 45KB/s
to say nothing changed, about 160MB for an hour with a tab open.

It now carries an ETag and answers `304` with no body when the caller
already has that version. Measured in a browser over 14 seconds: **7 polls,
638KB before, 2.1KB after.** The client keeps the tag itself and returns
early on a 304, so the saving is not just bandwidth — there is no JSON
parse, no new array, and no React re-render on those ticks.

Two details it depends on:

- **`Cache-Control: no-store`, not `no-cache`.** With `no-cache` the browser
  may satisfy the revalidation from its own cache and hand JavaScript a 200
  with a body — saving the bytes but not the parse or the re-render.
- **The run list is sorted.** `RunStore.List` ranges over a map, so the
  order was whatever Go felt like that iteration. Every client re-sorted
  anyway, but an unstable order also means an unstable body, which would
  have produced a fresh ETag every poll and quietly disabled the whole
  thing.

`refreshRuns()` drops the tag first, so an action you just took shows its
effect immediately instead of confirming nothing changed.

### The same trick, for navigating rather than polling

`GET /api/bots` is the other big one: 30KB, fetched on mount by the bot
library, Settings, the Team page, a swarm view and the builder. Nothing
polls it, so it cost nothing while you sat still and 30KB every time you
moved between those pages.

It carries an ETag now too, and `web/src/lib/botsCache.ts` holds the tag and
the last copy for the whole tab.

**It has nothing to invalidate, and that is the point.** Every read goes to
the server; the held copy is only ever returned when the server has just
answered 304, which it decides by hashing the body it would have sent. So a
bot whose demo/live switch was flipped a millisecond ago comes back flipped,
and no call site has to remember anything. The first version of this had an
`invalidateBots()` and three mutator wrappers around the calls that change a
bot — which guarded against staleness this shape of cache cannot have, and
made a save that changed nothing cost a full 30KB where it would otherwise
have been a 304.

Measured in a browser, the same five-page browse (Swarms → Team → Settings →
Runs → Swarms → a swarm), before and after both changes on this page:

| | before | after |
|---|---|---|
| `/api/bots` | 2 req, 61.0KB | 2 req, **30.5KB** (one 304) |
| `/api/runs` | 9 req, 241.7KB | 7 req, **80.6KB** |
| all of `/api` | 32 req, 335.9KB | 30 req, **144.3KB** |

The `/api/runs` half of that is a second bug the measurement found:
`GettingStarted` called `api.listRuns()` on every mount — a 91KB response —
to compute one boolean, "has anything ever run here". It reads the shared
`useRuns()` feed now, which was already fetching it.

`/api/swarms` is the largest one left untouched at 30.8KB over four
requests, for the same reason `/api/bots` was.

## The run log is live now

The landing page said "every step streams to a run log in real time". It
did not. Measured on a three-bot swarm: the log sat on three lines for
sixteen seconds and then produced five at once, because the agent buffered
every line in memory and wrote `<run>/log.jsonl` once, on exit. The runner
replayed that file after the container was already gone. `replayContainerLog`
said so in its own comment: "live per-step streaming for everything else is
a reasonable future enhancement, not attempted here."

Two halves:

- The agent flushes a line per step. `step.LogStreamer` is an *optional*
  interface on `Deps` — implement it and the interpreter hands over each
  line the moment it happens. Optional rather than a `Deps` method so the
  four existing implementations (demo, live, remote, recording) are
  untouched; only the agent, which can do something useful with a line
  mid-run, opts in.
- The runner follows the file while the container runs, every 300ms, and
  drains once more after it exits so a line written between the last poll
  and exit is never lost.

It reads the whole file each pass and emits from `consumed` onward rather
than holding a byte offset. The files are a handful of lines, and a decoder
that stops at the first error naturally skips a line the agent is halfway
through writing, picking it up whole on the next pass.

### What the timings actually say

With real per-step timestamps, a three-bot swarm on warm images:

```
+ 1.52s  container start, first step
+ 6.60s  ai.generate
+ 5.58s  memory.recall
+ 9.60s  ai.generate
+ 2.58s  render to PDF
= 27.1s  total
```

Roughly 16s of that is model round trips and 5.6s is a memory recall. A
container starts in 0.5-0.8s. There is no orchestration overhead worth
chasing here; the time is real work.

An earlier reading of the same swarm showed 24 seconds before the first
step. That was the harness image rebuilding because the Go source had just
changed, not a startup cost. Worth knowing when timing anything in this
repo: edit `internal/`, and the first run afterwards pays for a rebuild.

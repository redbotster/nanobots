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

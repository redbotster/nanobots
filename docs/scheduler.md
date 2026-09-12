# The scheduler

Every catalog swarm declares `trigger: {type: cron, expr: "...", timezone: "..."}` — "every weekday morning, recap my inbox." Until now, nothing in this build ever actually fired one; `cmd/nanobotd/main.go`'s own doc comment named this gap explicitly. `internal/scheduler` closes it: `nanobotd` polls `examples/swarms/` on an interval and executes any cron-triggered swarm that's due, through the exact same `Orchestrator.ExecuteSwarm` + `RunStore.Add` path a human clicking **Run** in the WebUI goes through — a scheduled run shows up in the Runs page identically to a manual one, including its own approval gates.

## How it works

- **No dependency** — a minimal, self-contained 5-field cron parser (`internal/scheduler/cron.go`), not a library. Supports `*`, exact numbers, ranges (`1-5`), steps (`*/30`), and comma lists — verified against every `trigger.expr` actually used in `examples/swarms/*.yaml`, not a generic spec built in a vacuum.
- **Polls, doesn't require a restart** — every tick (default 20s) re-scans the swarms directory, so adding a new swarm, editing a cron expression, or deleting a swarm takes effect on the next tick, not the next `nanobots up`.
- **Fires immediately if already due on first sight** — if a schedule already matches the instant it's discovered (nanobotd just started, or a swarm was just edited to "every minute"), it fires right then rather than silently waiting a full cycle. This is a deliberate choice, not an oversight: the alternative (always wait for the *next* occurrence) would mean editing a swarm to run more often could leave it looking broken for up to a full cycle.
- **One bad swarm never blocks another** — an unparseable cron expression, or a swarm that fails to launch, is logged and skipped; every other swarm's schedule keeps running.
- **Timezone-aware** — `trigger.timezone` (an IANA name, e.g. `America/Chicago`) is resolved via the standard library; an unknown timezone falls back to UTC with a logged warning rather than crashing the scheduler.

## A real operational note

Because it fires immediately when already due, starting `nanobotd` during a minute that already matches one of your swarms' schedules runs that swarm for real right then — the same way a real cron daemon re-reading its table mid-minute would. Every bot in this catalog ships on `connection: demo` by default, so this has no real-world side effects out of the box — but once you've flipped a bot's service to a live connection (see `docs/connections.md` and the Bot Library's per-service toggle), a scheduled swarm using it will genuinely send/post/pay for real, on its own, with no one watching. That's the entire point of a scheduler — just worth knowing plainly rather than discovering by surprise.

## Try it

Drop a swarm with `trigger: {type: cron, expr: "* * * * *"}` into `examples/swarms/`, run `nanobots up`, and watch the log:

```
scheduler: firing examples/swarms/your-swarm.yaml (due 2026-09-11T09:00:00-05:00)
```

`GET /api/runs` shows the resulting run exactly like a manually-started one — including a real approval gate opening if the swarm has one.


## Seeing a schedule without reading cron

Every cron-triggered swarm now says when it runs, in words, on its card and in its detail view — plus when it fires next:

```
⏰ Weekdays at 7:00 AM · in 2d
⏰ Every 2 hours on weekdays · in 30m
```

Before this the scheduler was invisible. It had been firing swarms since it was built, and nothing in the WebUI said a swarm was scheduled, when it ran, or when it would run again. A swarm that quietly emails you every weekday at 07:00 should not be a surprise.

`GET /api/swarms` carries `schedule` (the sentence), `schedule_expr` (the cron, kept because the sentence is a convenience and the expression is the truth), `timezone`, and `next_run_at`. The next-run time is computed with the same `scheduler.Parse` + `Schedule.Next` the scheduler itself runs on, so what the UI promises and what actually fires cannot disagree.

`scheduler.Describe` phrases the shapes the catalog uses and the ones people write by hand — times of day, day sets (`Weekdays`, `Weekends`, `Mon, Wed, Fri`), day-of-month (`Monthly on the 1st`), intervals (`Every 15 minutes`, `Hourly`), and intervals restricted to days (`Every 2 hours on weekdays`). Anything beyond that falls back to the raw expression rather than guessing: a wrong sentence about when something fires is worse than an honest cron string. Every phrase in the test table is also asserted to be something `Parse` accepts, so the UI can never describe a schedule the scheduler silently ignores.

A cron expression that *can't* be parsed now surfaces as `schedule_error` and renders in red on the card. Such a swarm never fires, and previously said so only in a daemon log line nobody reads.

## `7` means Sunday

Standard cron accepts both `0` and `7` for Sunday. This parser only allowed `0-6`, so `0 9 * * 7` failed to parse — and the scheduler's response to a parse failure is to log and skip, meaning the swarm silently never ran. `Parse` now normalises `7` to `0`, which keeps `matches` comparing against `time.Weekday()` (only ever 0-6).

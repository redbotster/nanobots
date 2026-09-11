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

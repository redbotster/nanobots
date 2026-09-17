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


## Triggers that aren't wired up

`internal/scheduler` only handles `type: cron`. Three catalog swarms declare something else — `lead-to-meeting` wants `webhook: website.form.submitted`, and `meeting-to-action` and `repurpose-everything` want `event: drive.file.created`. Nothing in this build fires any of them.

They used to render identically to a swarm with no trigger at all, which reads as "manual by design" rather than "its automation isn't built yet". `GET /api/swarms` now reports `trigger_type` for every swarm and `inert_trigger` for those two kinds, and the card says so:

```
⚡ drive.file.created — not wired up yet, so runs on demand
```

This is disclosure, not a fix: those swarms still only run when someone clicks Run.

## Making it survive a reboot

Twelve of the sixteen catalog swarms carry a cron trigger, and this
scheduler fires them — but only while nanobotd is running. Until now that
meant a terminal window someone remembered to leave open. Close the laptop
and the morning brief doesn't happen. A catalog written entirely in the
future tense has to survive a reboot.

```sh
go build -o bin/nanobots ./cmd/nanobots
./bin/nanobots service install       # writes a LaunchAgent, runs nothing
launchctl load -w ~/Library/LaunchAgents/dev.nanobots.nanobotd.plist
```

`service status` says whether one is installed and where; `service
uninstall` removes it. Deliberately three steps rather than one: this
writes a file into your LaunchAgents and the install command prints exactly
what it wrote, what it will run, where the logs go, and how to undo it —
then leaves loading it to you.

A few decisions worth knowing:

- **Per-user, not system-wide.** It goes in `~/Library/LaunchAgents`, not
  `/Library/LaunchDaemons`. nanobotd holds one person's credentials and runs
  their automations; it has no business running as root for everyone on the
  machine.
- **`RunAtLoad` and `KeepAlive`.** At login, and restarted if it dies — a
  scheduler that stops on the first crash is one nobody can rely on, and
  "every morning" has to survive a bad night.
- **The working directory is part of the job.** nanobotd resolves `bots/`,
  `examples/swarms/` and the harness Dockerfiles relative to it, so a job
  with the wrong one starts cleanly and finds nothing to run.
- **It refuses to install a `go run` binary**, which would work until the
  next reboot and then not, in a way nobody would connect back to this.
- **Docker still has to be running.** A bot is a container; a scheduler that
  survives a reboot and a Docker that doesn't gets you a run that fails at
  the first bot. Docker Desktop has its own "start at login".

### Missed runs don't fire late

A trigger that came due while nanobotd was off does not run when it starts:
each schedule's next occurrence is computed forward from now. That is
deliberate — a laptop opened after a week away should send one morning
brief, not seven — but it is not what everyone assumes, and assuming wrong
means quietly missing a run.

macOS only in this build. The plist generation is pure and tested against
`plutil`; a systemd unit is a small amount of work and belongs to someone
who can actually run it.

## A schedule that never works stops trying

Found by reading a real machine's run history: 198 runs, 135 failed, and
one swarm — `support-desk-lite`, on "every 30 minutes" — accounted for 85
of them. Forty-one of its runs held a container open for the full 30-minute
ceiling before being killed. Slack had never been connected, so every run
failed the same way, for about twenty hours of container time spent
re-learning one fact.

Nothing said so. The Runs page showed a wall of red with no sign it was one
fact repeated, and the card looked like any other swarm's.

After five consecutive failures a schedule stops firing. The card says so,
names the error, and offers **Try it again**:

```
⏸ Paused after 43 failed runs
item 1 of 1: container exceeded 30m0s and was stopped
[ Try it again ]
```

Below the threshold the schedule line warns instead — "· 3 failed in a row"
— so two in a row is visible before the fifth.

The pause is *derived from run history*, not kept as its own state machine.
History already survives a restart, so the pause does too: restarting the
daemon is not a reason to spend another twenty hours proving the same
point. The only thing stored is "the user pressed Try it again at time T"
(`~/.nanobots/state/agents/schedule-resumed.json`), after which failures are
counted afresh. A corrupt marker leaves schedules paused, which is the safe
direction — it can never start one firing behind your back.

One success anywhere in the streak clears it. This is for "never worked",
not "sometimes fails". A manual swarm never reports as paused, since it has
no schedule to stop — though its failure streak is still reported, because
it is still true.

`POST /api/swarms/{name}/schedule/resume` is the same thing from a script.
It does not run the swarm: resuming a schedule and triggering a run are
different intentions, and conflating them would mean you cannot un-pause
something without also firing it.

## The composer can schedule

Twelve of the sixteen catalog swarms carry a cron trigger, and until now
nothing you built could have one. `draftToNanoswarm` hardcoded
`trigger: {type: manual}`, so composing *"every friday summarise my overdue
invoices and email me the list"* produced a swarm the model had described as
**"Every Friday, finds overdue invoices and emails a summary"** — and that
would never once fire on a Friday. The description was true about the intent
and false about the thing.

The composer now emits a `schedule` field, and the prompt is explicit that
it must not describe a schedule it has not set. Against the live model:

| request | schedule |
|---|---|
| "every friday summarise my overdue invoices…" | `0 9 * * 5` |
| "every weekday morning recap my inbox" | `0 7 * * 1-5` |
| "draft a reply to this one email when I paste it in" | *(none)* |

The builder shows it before you save — a preset dropdown, or a raw cron
field with the expression described back to you as you type
(`GET /api/schedule/describe`). That endpoint uses the same
`scheduler.Parse` and `scheduler.Describe` the swarm list and the firing
loop use, so what the picker promises and what happens cannot drift. `0 9 *
* 5` reads back as "Fridays at 9:00 AM", which is the whole point: you asked
for Friday, and you can check you got Friday.

An expression the scheduler cannot parse is **refused before anything is
written**. A swarm saved with a broken schedule looks scheduled in the list,
reports no next run, and silently never happens — worse than being honest
about being manual.

### `schedule` is a tri-state, and it has to be

- **absent** — leave whatever trigger this swarm already has alone
- **`""`** — make it manual
- **`"0 9 * * 5"`** — this cron expression

The distinction is not pedantry. The builder does not model triggers, so
every save from it omits the field; if absent meant "manual", opening a
scheduled swarm and pressing Save changes would quietly unschedule it. That
is the same class of loss `swarmmerge.go` exists to prevent, and it now has
a test of its own.

## Why the scheduler is local, and stays local

1Claw has Automations — `POST /v1/automations`, with `trigger_type: cron`, a
timezone, and a workflow of typed steps. The obvious plan was to move the
catalog's twelve cron triggers onto them, so a schedule fires whether or not
your laptop is awake. Three things measured against the live API say the
local scheduler stays the default instead.

**An Automation cannot reach your machine.** Its only way back into nanobots
is the `http` step, and `GET /v1/automations/step-types` describes that step
as SSRF-validated. 1Claw's cloud calling `http://127.0.0.1:7474` is exactly
what SSRF validation exists to refuse. An Automation can only drive a
nanobots that is publicly reachable — a Cloud Runtime you deployed
(`docs/hosting.md`), not a laptop.

**Seven of the sixteen catalog swarms pause for a human.** The same endpoint
says `http` is *"Bounded by the 300s whole-run timeout rather than a
per-step one."* Five minutes is not how long it takes someone to answer an
approval; the local gate's own budget is thirty.

**`http` is `agent_allowed: false`.** An Automation that calls back into
nanobots has to be created with a human credential, so the composer cannot
propose one and have it work.

What that leaves is real but narrow: for a hosted deployment, running swarms
that never wait on a person, an Automation is a better cron than a machine
that has to stay on. That is opt-in, it is not built yet, and the honest
prerequisite is an image you have pushed
(`docs/1claw-feature-requests.md` #11) — so it waits on that rather than
shipping a command whose first step is missing.

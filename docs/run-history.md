# Run history

Every run this machine has ever finished is kept on disk, so restarting `nanobotd` — which you do every time you rebuild — no longer throws away what you ran. Before this, the Runs page had to apologize for it in its own copy.

## Where it lives

`~/.nanobots/nanobots.db` — one SQLite file, a `runs` table and a `run_log`
table, a sibling of the `blobs/` and `runs/` (container scratch)
directories `nanobotd` already owns:

```bash
sqlite3 ~/.nanobots/nanobots.db \
  "SELECT started_at, swarm_name, status, error FROM runs ORDER BY started_at DESC LIMIT 20"
```

This used to be `~/.nanobots/history/<run-id>.json`, one file per run,
deliberately not a database — a run is small, self-contained, and written
exactly once, so a directory of files was the whole feature. That stopped
being true at scale, measured rather than assumed: loading 1,000 runs with
realistic log sizes from the JSON format took **~59ms**, fine — but 1,000
runs with heavier, more log-chatty bots took **~1.02 seconds**, entirely
before `nanobotd` could answer its first request. The same reload from
SQLite takes **~29ms** for the realistic case and **~294ms** for the heavy
one (`internal/runner/scale_test.go` — these are real, measured numbers a
test enforces, not a one-time observation). One row per run, one child row
per log entry, so a run's log — the part most likely to be large — doesn't
have to be parsed to answer "how many runs are there."

The database holds the run's identity (`id`, `swarm_name`, `swarm_path`,
`triggered_by`), its outcome (`status`, `started_at`, `finished_at`,
`error`), its full log, and every bot's outputs.

### Migrating from the old JSON files

The first time `nanobotd` opens `nanobots.db` and finds it empty, it looks
for `~/.nanobots/history/*.json` and imports every run it finds, once,
logging `migrated N run(s) from .../history to .../nanobots.db`. **The JSON
files are never deleted by this** — a storage-format change doesn't get to
remove data as a side effect of moving it. They just stop being read again
after that first successful import; delete them yourself once you've
confirmed history looks right, or leave them, they cost nothing further.

## When it's written

Once, when the run reaches a terminal state (`succeeded` or `failed`) — `internal/runner.Run.SetStatus` fires a callback that `RunStore` registers in `Add`. An in-flight run has an empty log and no outputs, and writing it repeatedly as it progressed would buy nothing that survives a crash, because the run itself wouldn't.

So: **if `nanobotd` dies mid-run, that run leaves no record.** Stated plainly rather than papered over.

The write is one transaction — the run row and every one of its log rows commit together, or none of them do, so a crash mid-write can't leave a run with a status but half a log.

One ordering rule this depends on, enforced by a test (`TestErrorPersistsWhicheverOrderItIsSetIn`): the terminal status is what triggers the write, so `SetError` must come *before* `SetStatus(StatusFailed)`. It didn't originally, and failed runs persisted with a blank "why" — the one thing you come back to a failed run for. `SetError` now also re-fires the write if the run is already terminal, so either order ends up correct.

## What happens to a run that was still going

A run that was `running`, `pending`, or `awaiting_approval` when the process died is not resumable: its goroutine, its containers, and anyone waiting on its approval are all gone. On load it's restored as `failed` with `"nanobotd restarted while this run was in progress"`, rather than sitting in the list as `running` forever — which is what a naive reload would do.

## Bounds and failure modes

- **Capped at 1,000 runs** (`runner.MaxPersistedRuns`), newest kept — raised from 200 now that pruning is a `DELETE ... WHERE id NOT IN (...)` against an index rather than a directory listing. The number existed to stop unbounded growth "for years," per the original design; it was never observed as an actual performance limit at 200, and isn't one at 1,000 either (see the measurements above). Pruning happens on load, so the table can't grow for years unattended.
- **A row that won't decode is skipped**, not fatal — one bad run must not cost you the other 999. During migration, the same is true of a JSON file that won't parse.
- **An unreadable database is a warning, not a startup failure.** `NewPersistentRunStore` returns a usable store alongside the error, and `nanobotd` prints the warning and carries on.
- **History is per-machine and unencrypted.** A run's log and outputs are written as-is, so anything a bot printed is in there. The file is mode `0700`, in your home directory, and never leaves the machine — but it is not a vault. Credentials never appear in a run log (they stay in 1Claw or your secrets backend; see `docs/oneclaw-bridge.md` and `docs/secrets.md`), which is what makes that acceptable.

## The in-memory map is capped too

Everything above is about the database. `RunStore` also keeps a live map in
memory — every run this *process* has touched, so a run's subscribers,
pending approvals, and cancellation can live somewhere (none of those have,
or need, a disk representation). Until now that map only ever grew: nothing
ever removed a finished run from it, for as long as `nanobotd` kept running.

A swarm on a 30-minute schedule is ~17,500 runs a year, each holding its
full log and every bot's outputs — the same data `snapshotOf` writes to
disk, just never released from RAM.

`RunStore.evictOldestBeyondCap` now drops the oldest *finished* runs from
the map once it holds more than `MaxPersistedRuns`, the instant each one
reaches a terminal state — the same cap and the same moment the database
write already happens at. Only ever a finished run: one still in progress
owns real state (subscribers, pending approvals) eviction would corrupt,
and nothing prunes work that isn't done yet, however old it started.

An evicted run is not lost — `Get` falls back to the database for anything
the map no longer holds, reconstructing it the same way a full restart
already does (`snapshot.toRun()`). That reconstruction is never cached back
into the map, since doing so would just undo the eviction it came from: an
old run's detail page costs one small SQLite read each time it's opened,
which is the same trade the conditional-GET caches elsewhere in this app
make on purpose (see `docs/runs.md`).

## Try it

```bash
# Run something, restart the daemon, and see it still there.
curl -s -X POST localhost:7474/api/runs \
  -d '{"swarm_path":"examples/swarms/bookkeeping-assistant.yaml"}' > /dev/null
sleep 5
pkill -f 'nanobots up'; nanobots up &
sleep 3
curl -s localhost:7474/api/runs | jq -r '.[] | "\(.swarm_name) \(.status) \(.error // "")"'
```

The Runs page shows the same list, with the failure reason inline and a **Run it again** button on any finished run (that's what `swarm_path` is for).

## What a snapshot has to carry

A run is written here the moment it finishes and read back from here
forever after, so anything the snapshot leaves out is a fact the app has for
one process lifetime and then loses. Three were being left out, and each
turned into a lie on the way back:

| dropped | what the Runs page then said |
|---|---|
| `stopped_by_user` | a run you ended yourself came back a plain red **failed** |
| `nothing_to_do` | a watch that correctly found nothing came back another **succeeded** |
| `declined_by_user` | a declined approval stopped looking like a decision |

Found by measuring, not by reading: two scheduled `meeting-to-action` runs
had "nothing to do" in their logs and an empty field, because the daemon had
restarted in between. `TestHistoryKeepsWhatMakesAStatusMeanSomething` now
round-trips all three.

The rule this leaves behind: a field that changes how a finished run reads
belongs in `snapshot`, and the test above is where to add it. The precedent
was already in the file — `Captured` and `DemoServices` each carry a comment
saying exactly why they are persisted — it just was not followed for the
next three.

## Three stores, one retention rule

Everything under `~/.nanobots` that grows with runs is now bounded by the
same fact: the 1,000 runs history keeps.

| store | holds | pruned |
|---|---|---|
| `nanobots.db` (`runs` table) | the runs themselves | on load, to `MaxPersistedRuns` |
| `runs/` | per-run container workspaces | at startup, against the runs history kept |
| `blobs/` | the file contents runs produced | at startup, against the digests those runs still point at |

The blob store was the last unbounded one. Every PDF, chart and downloaded
attachment a run ever produced stayed on disk for good — reachable from
nothing the moment its run aged out. Measured on the machine this was found
on: 109 files and 2.9MB, which is small, and grows for as long as the
machine runs sixteen scheduled swarms.

Keyed by digest rather than by run id, because blobs are content-addressed:
two runs that produced identical bytes share one file, so references are
counted across every kept run before anything is deleted. Both a run's
outputs and its captured fixtures count as a reference — a run detail page
shows a file output as a download, and "turn this run into test data"
replays what each bot produced.

One rule rather than three that can disagree about what "old" means: a run
you can still open keeps its workspace and its files, and a run that has
aged out loses both at the same moment.

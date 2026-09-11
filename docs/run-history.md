# Run history

Every run this machine has ever finished is kept on disk, so restarting `nanobotd` — which you do every time you rebuild — no longer throws away what you ran. Before this, the Runs page had to apologize for it in its own copy.

## Where it lives

`~/.nanobots/history/<run-id>.json` — one file per run, a sibling of the `blobs/` and `runs/` (container scratch) directories `nanobotd` already owns. Deliberately not a database: a run is small, self-contained, and written exactly once, so a directory of files is the whole feature. No schema, no migrations, and you can read one with `cat`:

```bash
cat ~/.nanobots/history/*.json | jq -r '"\(.started_at)  \(.swarm_name)  \(.status)  \(.error // "")"'
```

The file holds the run's identity (`id`, `swarm_name`, `swarm_path`, `triggered_by`), its outcome (`status`, `started_at`, `finished_at`, `error`), its full log, and every bot's outputs.

## When it's written

Once, when the run reaches a terminal state (`succeeded` or `failed`) — `internal/runner.Run.SetStatus` fires a callback that `RunStore` registers in `Add`. An in-flight run has an empty log and no outputs, and writing it repeatedly as it progressed would buy nothing that survives a crash, because the run itself wouldn't.

So: **if `nanobotd` dies mid-run, that run leaves no record.** Stated plainly rather than papered over.

The write is a temp file plus a rename, so a crash mid-write can't leave a half-parsed file where a run used to be.

One ordering rule this depends on, enforced by a test (`TestErrorPersistsWhicheverOrderItIsSetIn`): the terminal status is what triggers the write, so `SetError` must come *before* `SetStatus(StatusFailed)`. It didn't originally, and failed runs persisted with a blank "why" — the one thing you come back to a failed run for. `SetError` now also re-fires the write if the run is already terminal, so either order ends up correct.

## What happens to a run that was still going

A run that was `running`, `pending`, or `awaiting_approval` when the process died is not resumable: its goroutine, its containers, and anyone waiting on its approval are all gone. On load it's restored as `failed` with `"nanobotd restarted while this run was in progress"`, rather than sitting in the list as `running` forever — which is what a naive reload would do.

## Bounds and failure modes

- **Capped at 200 runs** (`runner.MaxPersistedRuns`), newest kept. Pruning happens on load, so the directory can't grow for years unattended.
- **A file that won't parse is skipped**, not fatal — one bad run must not cost you the other 199.
- **An unreadable history directory is a warning, not a startup failure.** `NewPersistentRunStore` returns a usable store alongside the error, and `nanobotd` prints the warning and carries on.
- **History is per-machine and unencrypted.** A run's log and outputs are written as-is, so anything a bot printed is in there. It's mode `0700`, in your home directory, and never leaves the machine — but it is not a vault. Credentials never appear in a run log (they stay in 1Claw; see `docs/oneclaw-bridge.md`), which is what makes that acceptable.

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

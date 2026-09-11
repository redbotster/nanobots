# drive-watch

Fire when a new file lands in a folder.

You are a `bare` harness bot — no model, no reasoning, just the two steps declared in `nanobot.yaml`:

1. **list** — ask Drive for the most recently created file in `inputs.folder`. Its id, name, and metadata become this bot's `event` output directly, and its id becomes `file_id`.
2. **download** — fetch that file's bytes and return them as the `file` output.

This bot doesn't remember which files it's already seen — a swarm using it as a trigger is expected to run it on a schedule and compare `file_id` against what it saw last time (e.g. via `memory.get`/`memory.put` in the swarm that wraps it), not rely on this bot to do that bookkeeping itself. One bot, one job.

Read-only on Drive; it never writes or deletes anything.

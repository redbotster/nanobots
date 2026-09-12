# Turning a real run into test data

Every bot ships fixtures under `bots/<id>/fixtures/`, and they are what
`nanobots conform` and the Go test suite replay — no Docker, no network, no
model. That is why the whole catalog can be checked in a second.

They are also hand-written, which makes them somebody's guess at what a
model or an API returns. Guesses drift. A fixture written before a prompt
changed still passes while the real bot has been broken for weeks, and four
fixtures were hand-written in a single day of work on this repo — one of
them (`support-triage`'s `drafts.create`) wrong in a way only a real run
exposed.

So a finished run offers you the real thing.

## Using it

Open a succeeded run. Under **Keep this run as test data** is every fixture
that run would produce, marked *new* or *replaces what's committed*, with
the current file and the recorded one side by side. Tick what you want and
write it.

Anything that would overwrite committed test data starts unticked. The safe
half is the half you can take without reading.

It writes into `bots/<id>/fixtures/`, two-space indented and
newline-terminated — a diff a person can read. **Review it before
committing**: that is the point of the two-step, and a control that silently
rewrote test data would be the worst possible way to discover a fixture had
changed.

## What gets recorded

Exactly what a fixture holds, under exactly the filename `DemoDeps` reads it
back from — the names come from one function used by both, so a recorded
fixture cannot be filed somewhere conformance won't look:

| file | from |
|---|---|
| `ai.generate.json` | the model's response, parsed |
| `<service>.<op>.json` | a service call's result |
| `web.fetch.json` | a fetch |
| `memory.recall.json` | recall answers, keyed by question |

Approvals, memory writes and notifications are **not** recorded. They are
decisions and side effects, not test data — and a recorded "approved" would
turn a gate into a rubber stamp on every later run.

The model's response is stored parsed rather than raw, through the same
extraction the interpreter uses. So a fixture is exactly what the bot acted
on, including the rescue of an answer that arrived wrapped in prose.

Only the first item of a fan-out is recorded: a fixture describes one run of
a bot, and twenty would overwrite each other anyway.

## How it works

`step.RecordingDeps` wraps whatever a bot actually ran against. It cannot
change what the bot sees — every method returns the inner value untouched
and records afterwards — and it records failures nowhere, since a fixture of
a failed call is a fixture of a broken bot.

Captures are keyed by the **catalog** bot, not the swarm-local instance id: a
fixture belongs to `bots/review-board/`, not to whichever swarm called that
instance `board`. They are stored on the run and persisted with it, so the
offer survives a restart, with any single fixture over 256KB dropped — a
megabyte of scraped HTML is not readable test data and would bloat every run
snapshot in history to keep it.

## Verified

A real `supervisor-review` run against a live model captured
`ai.generate.json` for all three bots. Pinning `reviewer`'s replaced a
hand-written fixture with genuine model output, and both
`nanobots conform bots/reviewer` and the full catalog conformance suite
passed unchanged afterwards.

## Adopted from n8n, not copied

The idea is n8n's pinned node data. Only the idea — n8n is fair-code under
the Sustainable Use License rather than OSI open source, so nothing here
derives from its source. The difference in shape is that n8n pins data into
the workflow for iteration; here it is written out as committed test data,
because the thing worth keeping is a check that runs in CI.

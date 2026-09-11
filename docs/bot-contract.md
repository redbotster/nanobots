# The bot contract

A nanobot is a container that follows one contract. Any harness that can read a file and write a file can be a nanobot — that's the whole point (see `NANOBOTS-BLUEPRINT.md` §3.1).

## The contract

1. The bot's own files (`nanobot.yaml`, `bot.md`, `prompts/`, `templates/`, `fixtures/`) are mounted read-only at `/bot`.
2. Its resolved inputs — one JSON value per declared input port, already defaulted and validated by the planner/runner — are written to `/run/inputs.json` before the container starts.
3. The bot runs `cmd/nanobot-agent`, which:
   - loads `/bot/nanobot.yaml`,
   - executes `spec.steps` against those inputs (see `internal/step`),
   - writes one file per declared output port under `/run/outputs/`:
     - a **file**-typed port writes raw bytes to `/run/outputs/<port>` plus a `/run/outputs/<port>.mime` sidecar,
     - everything else is JSON-encoded to `/run/outputs/<port>.json`.
4. Exit code `0` means success; anything else means the bot failed, and `/run/log.jsonl` (one `{"step","msg"}` object per line) has the detail.

## What the container never gets

No bot container ever holds a real 1Claw credential, not even a short-lived one. Every step that needs the outside world — `service.call`, `ai.generate`, `web.fetch`, `memory.get`/`put`, `approve`, `notify` — is a callback to nanobotd instead, authenticated with a random per-run token that means nothing outside that one run. See `internal/step.RemoteDeps` and `docs/oneclaw-bridge.md`.

`transform.render` and `transform.now` run entirely inside the container — rendering needs no credential, so there's no reason to round-trip it through nanobotd.

## Conformance

`nanobots conform ./bots/<id>` (and the same logic under `go test`, see `internal/contract`) proves a bot honors its own declared ports without needing Docker or a network: it runs the bot's `spec.steps` in-process against `DemoDeps`, which serves fixture data from `./bots/<id>/fixtures/*.json` — one file per `<service-id>.<op>.json`, plus `ai.generate.json` and `inputs.json`. A bot whose declared output port is never produced, or comes back the wrong type, fails conformance. If a `file`-typed input port's actual bytes matter (e.g. a bot that reads the file's text for an `ai.generate` prompt, not just passes it through), add `fixtures/<port>.content` with real bytes — conformance seeds the blob store from it before running; without one, the port resolves to a fake, non-dereferenceable URI, which is fine for a bot that never reads the content.

## Try it

```
nanobots conform bots/recap-emails-to-pdf
nanobots conform bots/email-drive-file
nanobots run -f examples/swarms/daily-email-recap.yaml
```

The first two prove each bot's contract in isolation; the third runs both, for real, in Docker.

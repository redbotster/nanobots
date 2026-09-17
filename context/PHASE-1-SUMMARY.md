# Phase 1 summary: the first five minutes

Stopping here for review, as the plan asks.

Phase 1 was "someone who has never seen this can install it and watch
something work". Five items, one commit each, plus the acceptance test the
phase was supposed to end with.

## What shipped

| Item | Commit | What it is |
|---|---|---|
| 3. One binary | `9774e0e` | The WebUI is embedded in the binary, so `nanobots up` serves the app and the API on one port. Nothing runs in a second terminal. |
| 4. `nanobots init` | `bca7b4b` | First-run setup without a text editor: enrol an agent key or paste one, pick a model, or skip both and stay on example data. |
| 5. Docker optional | `56af1b3` | 34 of 39 bots run in-process. A container was protecting nothing: their steps are a declared list, and every step reaching the outside world already ran in `nanobotd`. |
| 6. `nanobots deploy 1claw` | `9a7675d` | A Cloud Runtime from a pushed image, behind JWT inbound auth, env from a vault environment. Reports its two real limits rather than letting you find them. |
| 7. `docker compose up` | `623f810` | The third way in, and the first with no toolchain at all. Plus a devcontainer and one Install section with a requirements table. |
| Acceptance | `763af0b` | `scripts/e2e/first-run.ts`: a real browser against a real `docker compose up`, nothing configured, running a swarm to success. Daily. |

Three more commits belong to the phase without being items: the README
split (`50032f6`), and two bugs the work turned up (`11ab813`, `a274558`).

58 files changed, 5901 insertions, 595 deletions.

## The measured claim

A machine with Docker and nothing else:

```
git clone … && cd nanobots && docker compose up
```

comes up healthy, serves the UI, and runs `supervisor-review` to
`succeeded` against fixtures. No 1Claw account, no model key, no OAuth app,
no Go, no Node. Verified, not asserted: that exact sequence is
`.github/workflows/first-run.yml`, daily, with screenshots on failure.

`nanobots health` inside the container says what it came up as:

```
ok  127.0.0.1:7474  model: none  1claw: not configured  docker: unavailable (the 5 browser bots cannot run)
```

## What running it found

Every one of these came from starting the thing, not from reading it.

**The Dockerfile described a key path that did not exist.** It said the
daemon reads its 1Claw key from the environment; it only ever read a dotenv,
which a container has no home directory to keep. The image built, ran, and
served the whole catalog on fixtures with nothing saying why. `LoadEnvValue`
now falls back to the process environment when no explicit path was given —
the file still wins when it has the key, and an explicit `--env` path never
falls back. `nanobots deploy 1claw`, shipped one commit earlier, had the
same hole.

**The volume arrived owned by root** and the nonroot daemon restart-looped
on `mkdir /data/.nanobots: permission denied`. Docker seeds a fresh named
volume from the image's contents at the mountpoint, ownership included, and
there was nothing at `/data`.

**A "Login" button stood in front of the whole product** and was not a
login: it flipped a React state, asked for nothing, protected nothing, and
had to be clicked again on every reload. Relabelled to what it does, and the
choice is remembered.

**`POST /api/runs` with no swarm name** answered `read /app/examples/swarms:
is a directory`, because `filepath.Base("")` is `"."`. The same family as
the `filepath.Base("..")` trap already written up in CLAUDE.md.

**CI had been red since item 6** and green on this laptop, because a test
read the developer's `~/.secrets/nanobots.env` and behaved differently
depending on whether a key was there.

**docs/hosting.md claimed compose runs the five browser bots.** It does not:
there is no Docker inside the container, and mounting the host's socket
would trade root-equivalent access to the machine for five bots.

## What the acceptance test nearly got away with

Worth reading before writing another one. Both were green ticks for work
that had not happened:

1. **Asserting a word that was already on the page.** "The run succeeded"
   was checked by looking for `succeeded` in the body text — which the
   *previous* run's status already put there. It passed in 559ms against a
   run taking twenty seconds.
2. **Waiting for a label that never renders.** The obvious fix, watching for
   `Running…` first, is also wrong: with no model configured every
   `ai.generate` returns its fixture, so the swarm finishes between two
   polls.

It now records the newest run id before the click, requires a *different*
run to reach a terminal state, and requires the page to be showing that run.

## What Phase 1 did not do

- **No release is tagged**, so `brew install` and `npx nanobots` still do
  not work. The GoReleaser config and the npx shim validate; nothing is
  published. This is the largest remaining gap between what the README says
  and what a stranger can do.
- **No published image**, so `nanobots deploy 1claw` still needs
  `--image <ref>` you built and pushed yourself. Both of these are one
  tagged release away and neither should be faked meanwhile.
- **The container cannot run the five browser bots**, and the honest fix is
  a second image with Docker available to it, not a socket mount.
- **A container with an existing 1Claw key collides with agents a host
  install already enrolled** — one agent per bot name, key shown once
  (`docs/1claw-feature-requests.md` #4). The run says so precisely; nothing
  resolves it automatically.

## Recommended next

Phase 0's finding still stands and still reorders Phase 2: **item 12
(approvals through the agent JWT) first**, then item 15 (agent quota, which
item 12 makes matter more). The largest single source of failed runs on this
machine is scheduled swarms whose approval nobody was awake to answer — 54
runs, 27 hours of container time — and the fix is a credential swap in code
that already builds the right request.

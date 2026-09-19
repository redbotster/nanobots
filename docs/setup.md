# Setup

`nanobots init` is the first run. It writes one file and asks at most three
questions, and skipping all of them still leaves a working install.

```
nanobots init
```

## What it writes

One dotenv file, `~/.secrets/nanobots.env` by default (`--env <path>` to put
it elsewhere), created at mode 0600. Nothing is written into this repo, and
nothing is written into a bot container.

That file is the only place a 1Claw key can live: everything else nanobots
holds goes into a 1Claw vault secret, and a vault cannot decrypt itself
without the key that opens it. `.devcontainer/devcontainer.json` deliberately
does not mount it in — a dev container starts with no key at all, the same
demo-data-only state a fresh clone starts in.

## The rest of the CLI

`nanobots plan`, `run` and `conform` (the README's Quick start) cover a
swarm's own lifecycle. Three more talk to a running daemon or to 1Claw
directly:

```sh
nanobots health                                           # is a running daemon answering?
nanobots agents                                           # the 1Claw agents this repo made, and which are unused
nanobots deploy 1claw --image <ref>                       # run it on a 1Claw Cloud Runtime
```

`health` is what a process manager should poll rather than guessing from
the port. `agents` exists because agent count is a real cap
(`docs/1claw-feature-requests.md` #4) — it's how you find and prune the ones
this repo made but nothing uses anymore. `deploy 1claw` is covered in full
in [docs/hosting.md](hosting.md).

## Where the key is read from

In order:

1. The file named by `--env <path>`, if one was given. Taken literally —
   nothing else is consulted, because "read the key from this file" has to
   mean that file.
2. Otherwise `$NANOBOTS_ENV_FILE`, or `~/.secrets/nanobots.env`.
3. Otherwise the process environment — `ONECLAW_API_KEY` and friends.

The file wins over the environment when it has the key. It is what `init`
writes and what Settings edits, so a stale exported variable silently
overriding the key you just saved would be the worse surprise.

The environment fallback exists for containers, which have no home directory
to keep a dotenv in and no way to get one there without baking a credential
into an image. `compose.yaml` and `nanobots deploy 1claw` both rely on it;
`docs/hosting.md` covers how each passes the value.

## The two kinds of 1Claw key

They are not equivalent, and `init` defaults to the narrower one on purpose.

| | agent key (`ocv_`) | Human key (`1ck_`) |
|---|---|---|
| Run bots, read and write its own vault | yes | yes |
| Ask you to approve something | yes | **no** — `/v1/approvals/request` refuses a Human key with "Only agents can request approvals" |
| Shroud (model calls, budget, redaction) | yes | yes |
| Install a connector, create a binding | no | yes |
| Decide or list approvals | no | yes |
| Read the org's security posture (`/v1/otel/*`) | no — 403, control-plane only | yes |
| Blast radius if the file leaks | that one agent | the whole 1Claw account |

The practical read: an agent key gives you a fully working install with one
degraded panel (Settings' posture row) and no connector installs. A Human
key gives you everything and puts full account access in a file on your
laptop. Start with an agent key.

## Enrolling an agent

Option 1 in `init` calls `POST /v1/agents/enroll`, which is public and needs
no credential. It returns an approval link, opens it, and waits.

**You will still paste a key.** 1Claw shows a new agent's key exactly once,
to whoever approves it in the browser, and there is no endpoint for the CLI
to collect it afterwards. So this is not a hands-free flow — the saving is
that what you paste is an agent key rather than a Human one. If that ever
changes, see `docs/1claw-feature-requests.md`.

Giving your account email sends the same link by mail; omitting it makes a
link-only enrolment you approve while signed in.

## Without 1Claw

Skip every question and every bot runs against this repo's example inbox,
invoices and files. That is a real way to use nanobots, not a broken one:
the swarms run end to end, the run log is real, approvals still gate real
actions, and nothing of yours is read or written.

Bots that generate text will return their example output rather than
thinking, unless you give `init` a provider key — `ANTHROPIC_API_KEY`,
`OPENAI_API_KEY` or `GEMINI_API_KEY` each work on their own. 1Claw's Shroud
is still the better backend, because it is the only one that bills against a
per-agent budget, redacts PII and secrets, and screens for injection
(`docs/llm.md`).

## Re-running it

Safe. `init` leaves an existing `ONECLAW_API_KEY` alone and says so rather
than asking again, so it is not a way to lose a working install. Change a
key by editing the file, or from Settings in the app.

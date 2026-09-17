# Hosting

Three ways to run nanobots. All three are the same binary and the same
catalog; they differ in what has to be installed and what can reach them.

| | needs | runs the 5 browser bots | fires schedules with the laptop shut |
|---|---|---|---|
| `nanobots up` | the binary | only if Docker is running | no |
| `docker compose up` | Docker | **no** — see below | only while the machine is on |
| `nanobots deploy 1claw` | a 1Claw account and a pushed image | only if the runtime has Docker | yes |

34 of the 39 bots need none of this — they run in the daemon's own process
(`docs/harnesses.md`). Docker and hosting are about the five that render
with a real headless browser, and about schedules that should fire when you
are not there.

## Locally

```
make build && ./bin/nanobots up
```

Binds `127.0.0.1` on purpose. The `/internal/steps/*` callback surface has
no authentication beyond a per-run token, because it was written on the
assumption that only this machine can reach it. Do not bind it to `0.0.0.0`
on a network you do not control.

## In a container

```
docker compose up
```

The image is this repo's `Dockerfile`: a distroless, non-root image carrying
the binary with the WebUI inside it plus the bot catalog. It sets
`--addr 0.0.0.0:7474`, because inside a container loopback means "this
container" and nothing could reach it — which moves the "who can reach the
callback surface" question to whoever deploys it. Publish the port to
`127.0.0.1` only, as `compose.yaml` does.

No credential is baked into the image, and there is no way to bake one in.
The daemon reads its 1Claw key from the environment at startup; everything
else lives in a 1Claw vault.

Verified on a machine with nothing configured: `docker compose up`, then
`supervisor-review` run through the API, finished `succeeded` against
fixtures.

### Four things the container does differently

**No Docker inside it, so the 5 browser bots do not run.** `/api/status`
reports `docker_available: false` and the run says so rather than failing
obscurely. Mounting the host's Docker socket would hand the container
root-equivalent access to your machine to gain five bots, which is not a
trade `compose.yaml` makes for you. Run those with `make build` locally.

**The state directory starts empty.** `HOME=/data` and a named volume hold
run history, blobs, memory and the agent credentials 1Claw hands back. If
you have already run nanobots on this machine with the same 1Claw key, the
container cannot reuse the agents your host install enrolled — 1Claw creates
one agent per bot name and shows its key exactly once
(`docs/1claw-feature-requests.md` #4). The run says exactly that, naming the
agent and the id. Either let it enrol its own under a different key, or
delete the unused agents.

**`localhost` in your dotenv means the container.** `HONCHO_URL`, an
`OPENAI_BASE_URL` pointed at Ollama, anything else running on your machine:
inside the container `localhost` is the container, and nothing is listening.
Use `host.docker.internal`. Nothing warns about this — the status endpoint
reports the backend that is *configured*, not one it has reached.

**The healthcheck is a real request.** Distroless has no shell, no curl and
no wget, so the only executable available is the binary itself:
`nanobots health --quiet` GETs `/api/status` and exits nonzero if nothing
answers. Deliberately not `nanobots version`, which prints a string compiled
into the binary and would report a dead daemon as healthy.

### Passing the key

`compose.yaml` reads `~/.secrets/nanobots.env` **on the host** and passes its
values in as environment variables — the file is never mounted and never
copied into the image. `required: false` on that `env_file` is what keeps
`docker compose up` working on a machine that has never run `nanobots init`.

That works because a key with no explicit `--env` path falls back to the
process environment (`docs/setup.md`). Point it somewhere else with
`NANOBOTS_ENV_FILE=/path/to/other.env docker compose up`.

## On a 1Claw Cloud Runtime

```
nanobots deploy 1claw --image ghcr.io/you/nanobots:v1
```

Creates a runtime, exposes the UI at `{slug}.run.1claw.co` behind JWT
inbound auth, and resolves environment variables from a vault environment so
no credential passes through the CLI. It shows the whole plan and asks
before creating anything, because a runtime bills.

Three limits worth knowing before you reach for it:

**You need your own image.** `GET /v1/runtimes/templates` returns nine —
python, node, hermes, openclaw, openclaude, opencode, claude-code, codex,
amp — all language runtimes or agent frameworks, none of which runs a Go
binary. Build and push this repo's `Dockerfile` somewhere 1Claw can pull
from. A `nanobots` template upstream would remove this step
(`docs/1claw-feature-requests.md`).

**Your local swarms do not travel.** The image carries the catalog and the
example swarms it was built with. 1Claw has no file-transfer API for a
runtime, so a swarm you wrote on your laptop is not there. Build your own
image with it in, or write it again in the hosted UI.

**Inbound auth is never `public`.** The command hard-codes `jwt`. The
callback surface is unauthenticated by design and exposing it to the
internet would be the worst thing this command could do.

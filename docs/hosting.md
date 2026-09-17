# Hosting

Three ways to run nanobots. All three are the same binary and the same
catalog; they differ in what has to be installed and what can reach them.

| | needs | runs the 5 browser bots | fires schedules with the laptop shut |
|---|---|---|---|
| `nanobots up` | the binary | only if Docker is running | no |
| `docker compose up` | Docker | **no** — see below | only while the machine is on |
| `nanobots deploy 1claw` | a 1Claw account | only if the runtime has Docker | yes |

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

### The UI is served out of the binary, compressed

`make ui` copies `web/dist` into `internal/webui/dist` and `//go:embed`
carries it into the binary, so `nanobots up` is one command and one port.

`http.FileServer` over an `embed.FS` gives you neither compression nor a
cache tag, and both were measured in a browser rather than guessed:

| | before | after |
|---|---|---|
| `index.html` | 1.2KB | **702B** |
| `assets/index-*.js` | 352.2KB | **110.1KB** |
| `assets/index-*.css` | 34.3KB | **7.0KB** |
| a cold load | 387.7KB | **117.8KB** |

Then the bundle itself, which was the other half. Radix was 39% of the
build's source bytes — more than React — and almost all of it arrived
through pages nobody had navigated to: the tooltip on a port badge pulls in
the whole of floating-ui, the YAML drawer's dialog pulls in
`react-remove-scroll` and a dismissable layer. Both live only in
`SwarmView`, which was the one page imported eagerly while its two siblings
were already lazy. Splitting it and the four secondary nav pages takes the
main chunk from 352.4KB to 195.7KB, and a cold load to **71.1KB**:

| | before | after |
|---|---|---|
| main chunk | 352.4KB | **195.7KB** (63.2KB over the wire) |
| a cold load | 387.7KB | **71.1KB** |

Each page's chunk is 1.5–6.6KB over the wire and arrives in 1–4ms off the
same machine, which is why `<LazyFallback>` is a line of text rather than a
spinner.

That is more than twice every API saving in this repo put together, and it
was paid on every load: an `embed.FS` file has a zero modtime, so net/http
emitted no `Last-Modified` and no `ETag`, and there was nothing for the
browser to revalidate against.

`internal/webui/serve.go` gzips and tags the whole build once at startup —
about 120KB of extra process memory against a compression pass on every
request. Everything under `/assets` is content-hashed by Vite, so it is
served `public, max-age=31536000, immutable` and a reload does not ask for
it at all; `index.html` is the file that *names* the hashed ones, so it is
`no-cache` and revalidates every time. A ranged request always gets the
uncompressed copy, because a range into compressed bytes is not the range
that was asked for.

## In a container

```
docker compose up
```

The image is this repo's `Dockerfile`, published multi-arch as
`ghcr.io/redbotster/nanobots` on every tag: distroless, non-root, carrying
the binary with the WebUI inside it plus the bot catalog. `compose.yaml`
builds it locally instead, so an edit to a bot takes effect without waiting
for a release; `docker run -p 127.0.0.1:7474:7474 ghcr.io/redbotster/nanobots:latest`
is the no-clone path. The image sets
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
nanobots deploy 1claw
```

Creates a runtime, exposes the UI at `{slug}.run.1claw.co` behind JWT
inbound auth, and resolves environment variables from a vault environment so
no credential passes through the CLI. It shows the whole plan and asks
before creating anything, because a runtime bills.

Three limits worth knowing before you reach for it:

**It runs a container image, not a template.** `GET /v1/runtimes/templates`
returns nine — python, node, hermes, openclaw, openclaude, opencode,
claude-code, codex, amp — all language runtimes or agent frameworks, none of
which runs a Go binary. So this deploys `ghcr.io/redbotster/nanobots`,
published on every tag, and `--image` overrides it with your own build. That
used to be a required flag and a build you had to do yourself.

**Your local swarms do not travel.** The image carries the catalog and the
example swarms it was built with. 1Claw has no file-transfer API for a
runtime, so a swarm you wrote on your laptop is not there. Build your own
image with it in, or write it again in the hosted UI.

**Inbound auth is never `public`.** The command hard-codes `jwt`. The
callback surface is unauthenticated by design and exposing it to the
internet would be the worst thing this command could do.

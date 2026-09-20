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

## Released builds

`v0.1.0` publishes binaries for macOS, Linux and Windows on both
architectures, each with the WebUI compiled in, plus the
`ghcr.io/redbotster/nanobots` container image. `brew install nanobots` and
`npx nanobots` do **not** work yet: the Homebrew tap repository does not
exist and the npm package is unpublished. The configuration for both
already exists — `.goreleaser.yaml` and `npm/` — each one secret (a tap repo
token, an npm publish token) away from working.

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

### The catalog is in the binary too, not just the UI

A single binary with the WebUI baked in still had nothing to show it: `up`
resolved `bots/` and `examples/swarms/` relative to the working directory,
which is empty air anywhere but this repo's own root. `brew install`,
`npx nanobots`, and a GitHub release binary run from `~/Downloads` all hit
this the same way — the UI would load and the Runs page would be real, but
the bot library and Swarms page would be empty.

`make catalog` copies `bots/`, `examples/swarms/` and `roles/roles.yaml`
into `internal/catalog/data`, and `//go:embed` carries them into the binary
next to the UI (`internal/catalog`). `nanobots up` checks for a `bots/`
directory next to where it was run first — a git checkout, the normal dev
case — and only reaches for the embedded copy when that's not there:

```
$ cd /tmp/anywhere && nanobots up
no git checkout found here — running this binary's own built-in catalog from ~/.nanobots/catalog
```

Extracted once to `~/.nanobots/catalog`, laid out exactly like a checkout's
root (`bots/`, `examples/swarms/`, `roles/roles.yaml`) — so every place that
already reads those relative to a resolved root needed no separate code
path, and a run started this way behaves identically to one started from
this repo's own directory. A content hash gates re-extraction, so a second
`nanobots up` against an unchanged binary is one file read, not a copy of
39 bots. Confirmed live: built the binary, ran it from an empty `/tmp`
directory with a fresh `$HOME`, and it served all 39 bots and 18 swarms and
ran `get-paid` — every bot in it in-process, none needing Docker — to
completion, with no checkout anywhere on the machine.

The five bots that need Docker (`meeting-prep`, `quote-builder`,
`recap-emails-to-pdf`, `render-pdf`, `sheet-reporter`) needed
`harness/*/Dockerfile` on disk to build their images from, and those
aren't embedded — `EnsureHarnessImage` looks for them relative to
`RepoRoot`, which is the extracted catalog directory here, not a real
checkout. See "The harness images are published too" below for how a
released binary gets them instead.

### The harness images are published too

`EnsureHarnessImage` now checks for `harness/*/Dockerfile` at `RepoRoot`
before doing anything else. When it's there — the normal case for anything
run from a git checkout — nothing changes: the local build and its
content-hash staleness check (above) behave exactly as before. When it
isn't — a standalone binary, whether it fell back to the embedded catalog
or was pointed at some other `--repo` with no `harness/` directory — it
pulls `ghcr.io/redbotster/nanobots-harness-{bare,openclaw}:vX.Y.Z` instead,
where `X.Y.Z` is this binary's own version (`nanobots version`). Built and
pushed by `.github/workflows/release.yml`'s `harness-images` job on every
tag, the same way the main image already is; `internal/contract`'s
`TestTheHarnessRegistryNamesMatchWhatReleaseActuallyPublishes` keeps the
image name in `internal/runner/docker.go` and the one the workflow
actually builds from drifting apart.

The version *is* the freshness check here — there's no source tree to hash
against, so a pulled image is trusted as long as its tag matches this
binary's version, and a different version pulls (and uses) its own
distinctly-tagged image rather than silently reusing whatever the last one
left behind.

Two things this doesn't cover: a plain `go build` (`nanobots version` says
`dev`) has no tagged release to pull, and fails with that named as the
reason rather than a bare Docker error. Confirmed live, on the same
standalone binary as above: a swarm using one of the five Docker bots
(`morning-brief`, whose `meeting-prep` and `render-pdf` both need it)
failed with `"...is not a released build (version \"dev\") that could
pull one instead — run from a nanobots checkout, where Docker can build it
locally"`, in the run's own error, not a mysterious Docker failure. And
until the first tagged release built after this change actually runs,
`ghcr.io/redbotster/nanobots-harness-bare` and `-openclaw` don't exist yet
to pull from at all — the same "not wired up yet" honesty the Homebrew
cask and npm package already get above, for the same reason: the code and
the workflow exist, but nothing has published through them yet.

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

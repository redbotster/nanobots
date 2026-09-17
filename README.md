# Nanobots

[![CI](https://github.com/redbotster/nanobots/actions/workflows/ci.yml/badge.svg)](https://github.com/redbotster/nanobots/actions/workflows/ci.yml)
[![First run](https://github.com/redbotster/nanobots/actions/workflows/first-run.yml/badge.svg)](https://github.com/redbotster/nanobots/actions/workflows/first-run.yml)

*Legos for AI. Snap micro-agents together, run them anywhere, keep the keys in 1Claw.*

Nanobots is a local-first system for composing single-job AI and
deterministic containers ("nanobots") into typed, DAG-shaped workflows
("nanoswarms"), with secrets, OAuth, LLM routing and guardrails delegated to
[1Claw](https://docs.1claw.co).

One binary serves the UI and the API. Nothing runs in a second terminal, no
account is required to start, and every bot ships answering from this repo's
own example data — so the first run does real work against fake data and
touches nothing of yours.

## Install

Three ways in. All of them end at <http://127.0.0.1:7474> with a working
catalog and nothing configured.

| | What you need | What you get |
|---|---|---|
| **Container** | Docker | Everything except the 5 browser bots, which need a Docker the container does not have |
| **From source** | Go 1.25+, Node 22+ | Everything, including the 5 browser bots if Docker is running |
| **Dev container** | Docker, VS Code or any devcontainer client | Everything, plus the toolchain and the verification pass ready to run |

**Container** — one command, nothing to install and nothing to clone:

```sh
docker run -p 127.0.0.1:7474:7474 ghcr.io/redbotster/nanobots:latest
```

Or from a clone, which is what you want if you will edit swarms:

```sh
git clone https://github.com/redbotster/nanobots && cd nanobots
docker compose up
```

**From source** — one binary that serves the UI and the API together:

```sh
git clone https://github.com/redbotster/nanobots && cd nanobots
make build          # WebUI + binary
./bin/nanobots init # optional: 1Claw and a model, or skip both
./bin/nanobots up
```

**Dev container** — open the repo in VS Code and *Reopen in Container*.
`.devcontainer/devcontainer.json` brings Go, Node and the host's Docker, and
installs both dependency sets on create. It deliberately does not mount your
`~/.secrets/nanobots.env`.

Then type what you want automated into the box at the top of **Swarms** and
press **Automate it** — or pick a ready-made swarm from the gallery below it
and press **Run**. Nothing has to be configured first: every bot answers
from this repo's example data until you connect an account.

**Docker is optional.** 34 of the 39 bots run in this process; the 5 that
need a container are the ones driving a real headless browser to render a
PDF or a chart, and the run log says which path each bot took, every run
([docs/architecture.md](docs/architecture.md)).

`make build` is `npm run build` plus `go build`. A plain `go build` also
works while developing — it produces a binary with no UI inside it, which
says so when you open it, and you run `cd web && npm run dev` alongside for
hot reload.

**Released builds.** `v0.1.0` publishes binaries for macOS, Linux and
Windows on both architectures, each with the WebUI compiled in, plus the
`ghcr.io/redbotster/nanobots` image above. `brew install` and `npx nanobots`
do **not** work yet: the Homebrew tap repository does not exist and the npm
package is unpublished. The configuration for both is in `.goreleaser.yaml`
and `npm/`, each one secret away.

```sh
nanobots plan -f examples/swarms/daily-email-recap.yaml   # type-check a swarm, print its DAG
nanobots run  -f examples/swarms/daily-email-recap.yaml   # run it, printing the log
nanobots conform bots                                     # check every bot honours the contract
nanobots health                                           # is a running daemon answering?
nanobots agents                                           # the 1Claw agents this repo made, and which are unused
nanobots deploy 1claw --image <ref>                       # run it on a 1Claw Cloud Runtime
```

Making it act on your real accounts is three optional layers — a model,
1Claw to hold the credentials, then one account at a time:
[docs/going-live.md](docs/going-live.md). Running it somewhere other than
your laptop is [docs/hosting.md](docs/hosting.md).

## Why nanobots, not one big agent

The obvious way to automate "recap my inbox, prep my meetings, and post my
content" is one big agent with a giant prompt and every tool bolted on. That
is also how you end up with something slow, expensive, impossible to debug
when it does the wrong thing, and terrifying to grant real
Gmail/Stripe/LinkedIn access to, because nothing bounds what it might decide
to do with all of them at once.

Nanobots takes the opposite bet — the Unix philosophy applied to AI
automation. Every bot in `bots/` is:

- **Small** — one job, statable in one sentence. `inbox-triage` sorts mail;
  it does not also draft replies (`draft-replies`) or send them
  (`email-send-approved`).
- **Fast** — most run with no LLM loop and no browser, and finish in
  seconds.
- **Powerful** — backed by a real integration wherever one exists, doing its
  one job completely rather than partially.
- **Modular** — every input and output is a typed, named port, so a bot
  never has to know anything about its neighbours beyond the shape of the
  data crossing the wire ([docs/bot-contract.md](docs/bot-contract.md)).
- **Orchestratable** — any bot snaps into any swarm the planner can
  type-check, and swaps for another with compatible ports without touching
  anything else.

That is the product: not a chatbot that does automation, but a growing,
composable catalog of small, real, individually-provable automation bricks —
plus an AI composer that does the assembly for you from one sentence of
plain English.

**Who it is for.** A busy solo operator — founder, indie hacker, anyone
running their own show without an assistant — who would rather describe the
outcome than configure automation software. The catalog also covers a small
business owner running light sales and ops, who is ready to graduate into
the bot library and the manual builder.

## The AI composer

Type what you want into the box at the top of **Swarms**:

> Help me automate a daily email recap and list it by priority

Under the hood (`internal/api/compose.go`, `POST /api/compose`): the real bot
catalog — every bot's id, description, and every port with its type and the
fields inside its json ports — goes to the model along with your message; the
answer is parsed into the exact shape the visual builder already saves; that
draft runs through the real planner, so a hallucinated bot id or a mismatched
port type comes back as a normal validation error rather than a silent bad
save; and the validated draft opens in the builder, pre-populated.

**It never saves or runs anything by itself.** Every proposed swarm goes
through the same human-reviews-it-first path as one built by hand.

If no combination of existing bots can satisfy the request, the composer says
so instead of guessing, and offers to escalate: a sandboxed coding agent
authors a brand-new bot, self-tests it against the real conformance runner,
and opens the same approval gate a swarm's `approve` step uses before the bot
ever joins the catalog. Approve it and the composer retries your original
request automatically ([docs/foundry.md](docs/foundry.md)).

A header toggle switches the whole UI between **basic** (Swarms, Runs,
Settings — nothing to learn before you can automate something) and
**advanced** (adds the bot library and the blank-canvas builder). It only
changes which entry points are visible; a swarm built either way executes
identically.

## The catalog

**39 bots** (`bots/`) — 33 job bricks plus 6 utility bricks (`approve`,
`notify`, `render-pdf`, `drive-save`, `drive-watch`, `form-to-sheet`), all at
`0.1.0`:

| Busy-person / solo-founder story (hero path) | SMB ops story (advanced) |
|---|---|
| `inbox-triage`, `draft-replies`, `follow-up-chaser`, `email-send-approved` | `lead-enricher`, `lead-router`, `quote-builder` |
| `meeting-prep`, `calendar-scheduler`, `meeting-notes-filer` | `invoice-chaser`, `receipt-filer`, `sheet-reporter` |
| `content-ideas`, `post-writer`, `x-thread-writer`, `post-publisher`, `repurposer`, `newsletter-drafter`, `tone` | `support-triage`, `review-responder`, `competitor-watch` |
| `x-mentions`, `linkedin-comments`, `comment-responder` | `linkedin-dm-triage` |
| `recap-emails-to-pdf`, `email-drive-file`, `github-issues-digest` | `review-board`, `reviewer`, `review-synthesis` ([docs/supervisors.md](docs/supervisors.md)) |

**16 swarms** (`examples/swarms/`), each with a header comment documenting
any place it simplifies the catalog's own aspirational diagram:

| Swarm | What it does |
|---|---|
| `daily-email-recap`, `daily-inbox-recap` | Recap the inbox to a PDF in Drive, notify or email the link. |
| `github-digest-to-slack` | Summarise a repo's newest issues and post the digest to Slack. |
| `inbox-autopilot` | Triage the inbox, draft replies to anything urgent, send them all once approved. |
| `morning-brief` | Triage the inbox and prep today's meetings into one brief. |
| `never-drop-a-thread` | Find sent threads that never got a reply, draft and send a nudge for every one. |
| `content-engine` | Brainstorm post ideas, write up the first one, publish it once approved. |
| `repurpose-everything` | Turn a new Drive file into posts across formats, publish once approved. |
| `listen-and-reply` | Pull new X mentions every weekday, draft replies to the ones worth answering, send the list. |
| `lead-to-meeting` | Log, enrich and route a new lead; draft a scheduling reply once approved. |
| `support-desk-lite` | Triage support mail, flag anything urgent to Slack, send every drafted reply once approved. |
| `bookkeeping-assistant` | File today's receipts and produce a spend report with a chart. |
| `get-paid` | Find every overdue invoice, send each reminder once approved, post one summary of what went out. |
| `meeting-to-action` | File a new transcript's notes, flag action items, draft follow-ups. |
| `weekly-client-report` | Build a client's spend report and email them the link once approved. |
| `supervisor-review` | A review board picks reviewers from your role library, each reviews in parallel, one synthesis reconciles them ([docs/supervisors.md](docs/supervisors.md)). |

What a bot and a swarm look like as files, in real YAML from this repo:
[docs/anatomy.md](docs/anatomy.md).

## What is actually built

The bot contract, the planner, the runner, the 1Claw bridge, seven direct
service clients, the composer, the foundry, the scheduler, approvals,
fan-out, run history and the WebUI are all real and exercised against live
APIs. Published packages are not: no release is tagged, so `brew` and `npx`
do not work yet. Container-level network egress is reported per bot rather
than enforced. No bot ships connected to a real account — every one starts on
`connection: demo` until a human deliberately flips it.

The full list, and the line-by-line table of what is real versus simulated,
is [docs/status.md](docs/status.md). The gaps that are 1Claw's rather than
ours — what each one blocks and the honest workaround shipped meanwhile — are
[docs/1claw-feature-requests.md](docs/1claw-feature-requests.md).

## Documentation

[**docs/**](docs/README.md) has one page per concept, each ending in how to
run it for real. The ones most people want first:

| | |
|---|---|
| [setup.md](docs/setup.md) | `nanobots init`, and the two kinds of 1Claw key |
| [anatomy.md](docs/anatomy.md) | what a bot and a swarm look like |
| [going-live.md](docs/going-live.md) | from demo data to real accounts |
| [architecture.md](docs/architecture.md) | how the pieces fit, and why no credential reaches a container |
| [bot-contract.md](docs/bot-contract.md) | the whole interface a bot honors |
| [connections.md](docs/connections.md) | every connection method, per provider |
| [scheduler.md](docs/scheduler.md) | cron triggers and the circuit breaker |
| [approvals.md](docs/approvals.md) | the gate, and answering from your phone |
| [testing.md](docs/testing.md) | the verification pass, and the claims that check themselves |
| [status.md](docs/status.md) | what works today, real vs. simulated |

The full product spec lives in
[`context/NANOBOTS-BLUEPRINT.md`](context/NANOBOTS-BLUEPRINT.md) and
[`context/NANOBOTS-CATALOG.md`](context/NANOBOTS-CATALOG.md). Treat those as
the source of truth for the YAML schemas and the launch catalog; everything
under `docs/` covers what is actually built, and if it contradicts the code,
the code wins.

## Contributing

`CLAUDE.md` is the working style, and the short version is: never let the app
say something that is not so, measure before improving, verify in a real
browser, and keep the docs current in the same commit as the change. The full
verification pass:

```sh
go build ./... && go vet ./... && go test ./... -race
cd web && npx tsc -b && npm run lint && npm run format:check && npm run test
```

The second badge above is `scripts/e2e/first-run.ts`: a real browser, a real
`docker compose up` with nothing configured, running a swarm to success —
the claim this README opens with, checked daily rather than asserted
([docs/testing.md](docs/testing.md)).

## License

Source-available, **not** open source. Nanobots is distributed under the
[Sustainable Use License](LICENSE.md) — the same license n8n uses — with one
deliberate change: n8n permits use "for your own internal business purposes",
and this one does not.

Non-commercial and personal use, and evaluation, are free. **Any commercial
use requires express written permission** from Kevin Jones Enterprises Inc.,
the author of Nanobots.

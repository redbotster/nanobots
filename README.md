# Nanobots

*Legos for AI. Snap micro-agents together, run them anywhere, keep the keys in 1Claw.*

Nanobots is a local-first system for composing single-job AI/deterministic containers ("nanobots") into typed, DAG-shaped workflows ("nanoswarms"), with secrets, OAuth, LLM routing, and guardrails delegated to [1Claw](https://docs.1claw.co).

The full product spec lives in [`context/NANOBOTS-BLUEPRINT.md`](context/NANOBOTS-BLUEPRINT.md) and [`context/NANOBOTS-CATALOG.md`](context/NANOBOTS-CATALOG.md). Treat those as the source of truth for the YAML schemas and the launch catalog; this README covers what's actually built. `docs/` has one page per concept — contract, connections, harnesses, approvals, the 1Claw bridge, Browser Bridge — each ending in how to run it for real.

## Status

This repo implements **Phase 0 + a working slice of Phase 1/2** of the blueprint's roadmap, end to end and verified live: the bot contract, planner, a real Docker-backed local runner, a real 1Claw bridge (Human API + Shroud + Browser Bridge), a REST+SSE API, and a WebUI — scoped to two example bots (`recap-emails-to-pdf`, `email-drive-file`) and one example swarm (`daily-email-recap`). The other 26 catalog bricks and 11 swarms, the `kubernetes`/`apple` compile targets, a real dynamic agent loop (see `docs/harnesses.md`), and the hosted control plane are not built yet.

`nanobots run -f examples/swarms/daily-email-recap.yaml` runs both bots as real, non-root, read-only-filesystem Docker containers; gets a real Shroud-generated recap through a real 1Claw agent; renders a real PDF; blocks on a real approval gate (answered from the terminal or the WebUI); wires one bot's output into the next bot's input across two separate containers; and finishes successfully. Gmail/Drive calls run against fixture data rather than real Google accounts — see `docs/connections.md` for why (short version: Google blocks sign-in outright on any automation-controlled browser, so the obvious Browser Bridge approach hit a real wall; that's documented, not hidden).

## Repo layout

```
cmd/nanobotd/     Go daemon — REST+SSE API, binds loopback only
cmd/nanobots/     Go CLI — plan, conform, schema, up, run (init/add/save/publish/compile: not yet)
cmd/nanobot-agent/ the container entrypoint every harness image runs
internal/schema/  Nanobot/Nanoswarm Go types, YAML loading, JSON Schema generation
internal/planner/ resolves a swarm's bots, type-checks snaps, builds/cycle-checks the run DAG
internal/step/    the universal step interpreter + Demo/Live/Remote Deps backends
internal/contract/ the conformance test runner (`nanobots conform`)
internal/oneclaw/ real 1Claw Human API + Shroud + Browser Bridge client
internal/runner/  Docker-backed orchestrator: builds harness images, runs bots, wires I/O
internal/api/     REST+SSE handlers, incl. the container callback endpoints
internal/daemon/  wires the above together; shared by cmd/nanobotd and `nanobots up`
harness/          Dockerfiles for the bot runtime images (bare, openclaw)
schemas/          Generated JSON Schema for Nanobot / Nanoswarm
bots/             Individual nanobots (nanobot.yaml + instructions + fixtures)
examples/swarms/  Example nanoswarms
web/              WebUI — Vite + React + TypeScript + Tailwind + Radix primitives
docs/             Concept docs, each ending in how to run it for real
```

## Quick start

```
go build ./...
go test ./...

# type-check the example swarm and print its run DAG
go run ./cmd/nanobots plan -f examples/swarms/daily-email-recap.yaml

# run it for real (needs Docker running)
go run ./cmd/nanobots run -f examples/swarms/daily-email-recap.yaml

# or drive it from the WebUI instead
go run ./cmd/nanobots up &
cd web && npm install && npm run dev
```

`nanobotd` reads your 1Claw Human API key from `$NANOBOTS_ENV_FILE` (default `~/.secrets/nanobots.env`, `ONECLAW_API_KEY=...`) at startup only — it's never written into this repo, logged, or handed to a bot container (see `docs/oneclaw-bridge.md`). Without a key, every bot runs fully in demo mode.

## License

MIT

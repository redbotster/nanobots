# Nanobots

*Legos for AI. Snap micro-agents together, run them anywhere, keep the keys in 1Claw.*

Nanobots is a local-first system for composing single-job AI/deterministic containers ("nanobots") into typed, DAG-shaped workflows ("nanoswarms"), with secrets, OAuth, LLM routing, and guardrails delegated to [1Claw](https://docs.1claw.co).

The full product spec lives in [`context/NANOBOTS-BLUEPRINT.md`](context/NANOBOTS-BLUEPRINT.md) and [`context/NANOBOTS-CATALOG.md`](context/NANOBOTS-CATALOG.md). Treat those as the source of truth for the YAML schemas and the launch catalog; this README covers what's actually built.

## Status

This repo currently implements **Phase 0 + a working slice of Phase 1/2** of the blueprint's roadmap: the bot contract, planner, local runner, a real 1Claw bridge, and a WebUI — scoped to two example bots (`recap-emails-to-pdf`, `email-drive-file`) and one example swarm (`daily-email-recap`). The other 26 catalog bricks and 11 swarms, the `kubernetes`/`apple` compile targets, and the hosted control plane are not built yet.

Gmail/Drive access in the two example bots runs against a **demo provider** (fixture data) rather than real Google accounts — real access was attempted via [1Claw Browser Bridge](https://docs.1claw.co/docs/agents/browser-bridge) driving a real logged-in browser, but Google blocks sign-in outright on any CDP/automation-controlled Chrome instance. That's a deliberate Google policy, not a bug here, so it's deferred pending either a Google OAuth client or added Gmail scopes on 1Claw's own Google OAuth provider. Browser Bridge itself works fine and is still the intended connection method for other services (Stripe, HubSpot, X/LinkedIn, etc.) in later tranches.

## Repo layout

```
cmd/nanobotd/    Go controller — REST+SSE API, planner, scheduler, context bus, compiler, 1Claw bridge
cmd/nanobots/    Go CLI — init, plan, up, run, save, compile
harness/         Dockerfiles for the bot runtime images (bare, openclaw)
schemas/         Generated JSON Schema for Nanobot / Nanoswarm
bots/            Individual nanobots (nanobot.yaml + instructions + fixtures)
examples/swarms/ Example nanoswarms
web/             WebUI — Vite + React + TypeScript + Tailwind + Radix primitives
docs/            Concept docs, each ending in a runnable swarm
```

## Quick start

```
go build ./...
go test ./...
./bin/nanobots plan -f examples/swarms/daily-email-recap.yaml
./bin/nanobots up
```

`nanobotd` reads your 1Claw Human API key from `$NANOBOTS_ENV_FILE` (default `~/.secrets/nanobots.env`, `ONECLAW_API_KEY=...`) at startup only — it's never written into this repo or logged. Without a key, Nanobots runs fully in demo mode.

## License

MIT

# The 1Claw bridge

`internal/oneclaw` is a real client for 1Claw's Human API — verified against the live API with a real key and cross-checked against `@1claw/openapi-spec`, not guessed from prose docs. This page is the map of what talks to what and why.

## Two different hosts, two different credentials

| Host | Auth | Used for |
|---|---|---|
| `api.1claw.co` | `ONECLAW_API_KEY` (`1ck_...`) → `POST /v1/auth/api-key-token` → short-lived bearer token | vaults, agents, execution intents, memory, approvals, Browser Bridge pairing |
| `shroud.1claw.co` | `X-Shroud-Agent-Key: <agent_id>:<agent_api_key>` (the agent's own long-lived `ocv_...` key) | `ai.generate` steps |

The Human API key lives only in `nanobotd`'s process memory, loaded once at startup from `$NANOBOTS_ENV_FILE` (default `~/.secrets/nanobots.env`) and never logged, written into this repo, or handed to a bot container. Each agent's `ocv_...` key is shown exactly once at creation; `internal/oneclaw/state.go` persists it to `~/.nanobots/state/agents/<name>.json` (mode 0600) so it survives a restart — if that file is ever lost, the agent has to be deleted and recreated, because 1Claw can't reissue it.

## What nanobotd does with it, per bot

For each bot instance in a run, `internal/runner.Orchestrator` calls `EnsureAgent` — find-or-create a 1Claw agent named `nanobots-<bot-name>`, with `shroud_enabled: true` and a `shroud_config` built from the bot's own `spec.guardrails` (`pii`, `injection_threshold`, `daily_budget_usd`) and `spec.model.provider`. That agent's id + key become the `LiveDeps` a bot's steps run against — see `docs/bot-contract.md` for how a step actually reaches this from inside a container.

## A gap that got fixed mid-build

`memory_enabled` originally showed up on `GET /v1/agents` responses but wasn't a settable field on either `CreateAgentRequest` or `UpdateAgentRequest` in `@1claw/openapi-spec@0.61.0`, so a normally-created agent 403'd on its first `memory.put`. Filed with the exact repro; the 1Claw team shipped `memory_enabled` on both request schemas in `0.61.1`. `internal/runner.agentRequestFor` now sets it on every new agent, and `Client.UpdateAgent` (`PATCH /v1/agents/{agent_id}`) exists to flip it on an agent created before the fix — used once, live, to fix the two example bots' own agents.

`memory.get`/`memory.put` steps still degrade gracefully (log a warning, continue) rather than fail a run outright if a memory call ever fails for some other reason — it's "since last run" bookkeeping, not a correctness requirement, so that defense stays even though the root cause here is fixed.

## Try it

```
NANOBOTS_LIVE_TEST=1 go test ./internal/oneclaw/... -run TestLiveReadOnlySmoke -v
nanobots run -f examples/swarms/daily-email-recap.yaml
```

The first is a read-only smoke test against your real account (lists vaults/agents, changes nothing). The second is the real thing: both bots run as real 1Claw agents, `ai.generate` goes through real Shroud, and `approve` opens a real run-level approval.

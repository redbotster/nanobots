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

## A real limit worth knowing about: the account's agent cap

`EnsureAgent` finds-or-creates by name, so it's idempotent across runs — but every distinct bot name in every swarm you've ever run against this account eventually gets its own agent, and 1Claw plans cap how many an account can hold (10 on the pro tier this was built against). Running out shows up as a 403 `"Agent limit reached"` from `EnsureAgent`, right when a swarm tries to run a bot whose agent doesn't exist yet. **Settings now shows the number before you hit it** — the Posture row reads `100/100 · 25/50 agents` with a line saying how many this app made, and turns amber at 80% of the allowance (`GET /api/posture`, from 1Claw's OTel summary plus `/v1/billing/subscription`). There's no code-level workaround — and this codebase never deletes an agent on its own, since agent memory (`memory.get`/`memory.put`'s "since last run" state) lives on it — but freeing a slot is safe: delete an agent you don't need (`Client.DeleteAgent`, or 1Claw's own dashboard) and the next run against that bot name just creates a fresh one.

## Try it

```
NANOBOTS_LIVE_TEST=1 go test ./internal/oneclaw/... -run TestLiveReadOnlySmoke -v
nanobots run -f examples/swarms/daily-email-recap.yaml
```

The first is a read-only smoke test against your real account (lists vaults/agents, changes nothing). The second is the real thing: both bots run as real 1Claw agents, `ai.generate` goes through real Shroud, and `approve` opens a real run-level approval.


## Which bots get a 1Claw agent

Not all of them. A bot gets its own agent only when it actually needs one:

- it has an `ai.generate` step (proxied through that agent's Shroud credentials), or
- it has a `memory.*` step (memory is namespaced per agent), or
- it has a **live** (non-demo) service whose provider has no native client in this build, so the call goes through 1Claw's generic binding — which is addressed by agent id.

Approvals are deliberately not on that list: `BuildDeps` always installs the local `RunQueueApprover`, so an `approve` step never touches 1Claw's own queue.

This used to be "every bot, always", and it was a real problem rather than just waste. Ten of the thirty catalog bots — `approve`, `drive-save`, `drive-watch`, `email-drive-file`, `email-send-approved`, `form-to-sheet`, `lead-router`, `notify`, `post-publisher`, `render-pdf` — are purely deterministic and never call an LLM, yet each burned one of the account's agent slots to never use it. On a pro tier that cap is 10, so a workspace with a handful of personal agents couldn't run a five-bot swarm. It also cost every one of those bots an agent-creation round-trip on its first run.

`internal/runner.needsOneClawAgent` is the predicate, and `TestRealCatalogNeedsFarFewerAgentsThanItHasBots` asserts it against the real catalog, so the number moving is something a test notices rather than something you discover at the cap.

**The cap is still real, though.** Twenty bots that do need agents still exceeds ten. Running the whole catalog live on a pro tier means either deleting agents between runs or upgrading the plan.

## Stale agent credentials heal themselves

An agent's `api_key` is shown exactly once, so `EnsureAgent` caches it under `~/.nanobots/state/agents/<name>.json`. That cache used to be trusted blindly, which meant an agent deleted on 1Claw left a dead key behind and every run of its bot failed with:

```
shroud: chat failed (401): agent key exchange failed: vault returned 401: Invalid credentials
```

Nothing in that message suggests the fix is "delete a local file". `EnsureAgent` now checks the saved credential against the agent listing (cached 30s, so a five-bot swarm makes one API call) and creates a replacement when the agent is gone.

It never deletes the stale credential first. A key that can't be recovered is worth more than the inconvenience being fixed, so the file is only ever overwritten by a *successful* replacement — if creation fails (the cap, a network blip, a listing that was momentarily wrong), the old credential is still exactly where it was.

## When the vault is locked

1Claw's vault re-locks periodically and requires passkey verification. Reading any secret then returns:

```
403 {"type":"about:blank","title":"Forbidden","status":403,
     "detail":"Passkey verification required to access vault secrets. Unlock with your passkey."}
```

This is a completely different problem from "this account was never connected", with completely different advice — but every caller used to report it as the latter (*"no connected account yet — connect it from Settings"*), sending you off to redo a connection that was already fine. `oneclaw.AsVaultLocked` classifies it, and the Slack/GitHub/Google/X/LinkedIn token paths all surface 1Claw's own sentence instead:

```
1Claw vault is locked: Passkey verification required to access vault secrets. Unlock with your passkey and retry.
```

It matches on a 403 whose body mentions a passkey, because 1Claw returns `"type":"about:blank"` here and there's no machine-readable code to key off. If that ever gains a real type, switch to it and drop the string match.

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

`EnsureAgent` finds-or-creates by name, so it's idempotent across runs, and 1Claw plans cap how many agents an account can hold (10 on the pro tier this was built against). Running out shows up as a 403 `"Agent limit reached"` from `EnsureAgent`, right when a swarm tries to run a bot whose agent doesn't exist yet.

This used to be a cap you could reach by writing bots: every distinct bot name got its own agent, and this account reached 27. Agents are keyed by guardrail profile now (below), so the catalog wants a handful rather than one per bot — but the cap is still what to watch when you connect many accounts or share a plan. **Settings shows the number before you hit it**: the Posture row reads `100/100 · 25/50 agents` with a line saying how many this app made, and turns amber at 80% of the allowance (`GET /api/posture`, from 1Claw's OTel summary plus `/v1/billing/subscription`).

This codebase never deletes an agent on its own — agent memory lives on it — but `nanobots agents --prune` will, after printing every name and asking.

## Try it

```
NANOBOTS_LIVE_TEST=1 go test ./internal/oneclaw/... -run TestLiveReadOnlySmoke -v
nanobots run -f examples/swarms/daily-email-recap.yaml
```

The first is a read-only smoke test against your real account (lists vaults/agents, changes nothing). The second is the real thing: both bots run as real 1Claw agents and `ai.generate` goes through real Shroud. `approve` opens a run-level approval in this app **and** in your 1Claw queue — see below.


## Which bots get a 1Claw agent

Not all of them, and no longer one each.

A bot needs an agent at all only when it does one of three things:

- an `ai.generate` step, proxied through that agent's Shroud credentials;
- a `memory.*` step, since memory entries hang off an agent;
- a **live** (non-demo) service whose provider has no native client in this
  build, which reaches the provider through 1Claw's generic binding —
  addressed by agent id.

`internal/runner.needsOneClawAgent` is that predicate. Ten of the catalog's
bots are purely deterministic and need none.

**Which agent** is a separate question, and the answer used to be "its own,
named after it". That is how this account reached 27 agents against a plan
cap of 50, all but one created by this repo, with `Agent limit reached`
waiting mid-run for anyone who added a few more bots.

Agents are now keyed by **guardrail profile**:

```
nanobots-redact-e7ecfa    16 bots
nanobots-allow-fd69d1     11 bots
```

The obvious alternative — one agent for everything — was rejected because an
agent *is* the guardrail boundary. `shroud_config` (PII policy, injection
threshold, allowed providers, daily budget) is set per agent, and 1Claw's
chat request body carries no per-call override; checked against the live
OpenAPI, `SendChatMessageRequest` has no policy fields. One agent would mean
one policy for every bot.

So two bots that declare the same guardrails share an agent, and a bot that
declares anything different gets its own automatically. The readable half of
the name says what the policy is; the six hex characters are a hash of the
whole profile, which is what stops two profiles that differ only in, say,
daily budget from colliding on one name and running under each other's
limits.

Measured against the real catalog before choosing: the twenty-seven bots
that need an agent declare exactly two distinct profiles. Twenty-seven
agents down to two, with nothing given up.

Four fixed agents sit alongside them — `nanobots` (approvals, and what a
hosted deploy runs as), `nanobots-composer`, `nanobots-shroud-proxy` and
`nanobots-foundry`. Every name this repo creates is registered in
`internal/agentname`, which is the list `nanobots agents` reads.

### Cleaning up what the old scheme left

Renaming the scheme does not tidy the account: the old per-bot agents keep
existing and keep counting.

```
nanobots agents
```

lists what is on the account in three groups — in use, made by nanobots and
now unused, and not made by nanobots (shown, never touched).
`nanobots agents --prune` offers to delete the middle group, printing every
name first and requiring an explicit yes. It is deliberately not automatic:
an agent's `api_key` is shown exactly once, so a wrong deletion cannot be
undone by anyone.

Pruning also drops the saved credential file for each agent it deletes,
which is what stops `EnsureAgent` reporting a mismatch for an agent that is
no longer there.

If you used the `1claw` memory backend, read the migration note in
[memory.md](memory.md) before pruning: entries written under the old agents
stay there and do not follow.

**The cap stops being the thing you plan around.** The whole catalog now wants two agents plus four fixed ones, so a pro tier's ten is no longer something a five-bot swarm can walk into.

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

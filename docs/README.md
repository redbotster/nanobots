# Nanobots documentation

One page per concept. Every page ends in how to run the thing for real,
because a concept you cannot exercise is a concept you cannot check.

Start at the [README](../README.md) for what nanobots is and how to install
it.

## Start here

| Page | What it answers |
|---|---|
| [setup.md](setup.md) | `nanobots init`, what it writes, and the two kinds of 1Claw key |
| [anatomy.md](anatomy.md) | what a bot and a swarm actually look like, in real YAML |
| [going-live.md](going-live.md) | from demo data to real accounts, and what each swarm costs to make real |
| [architecture.md](architecture.md) | how the pieces fit, and why no credential reaches a container |
| [status.md](status.md) | what works today, and what is real vs. simulated |

## Building things

| Page | What it answers |
|---|---|
| [bot-contract.md](bot-contract.md) | the whole interface a bot has to honor, and every step type |
| [builder.md](builder.md) | the visual canvas, zoom, and how a layout stays honest |
| [foundry.md](foundry.md) | what happens when the catalog genuinely cannot do it |
| [team.md](team.md) | a persistent, role-scoped coding agent, and why it needs no new approval gate |
| [lab.md](lab.md) | talking to your Team from one chat tab, and two real bugs found verifying it |
| [fixtures.md](fixtures.md) | turning a real run into a bot's test data |
| [supervisors.md](supervisors.md) | a review board that picks its own reviewers |
| [sharing.md](sharing.md) | exporting a swarm and importing someone else's |

## Running things

| Page | What it answers |
|---|---|
| [runs.md](runs.md) | the live log, stopping a run, and what each status means |
| [run-history.md](run-history.md) | what survives a restart |
| [scheduler.md](scheduler.md) | cron triggers, timezones, and the circuit breaker |
| [webhooks.md](webhooks.md) | starting a run from outside |
| [approvals.md](approvals.md) | the gate, what it tells you, and answering from your phone |
| [parallelism.md](parallelism.md) | which swarms branch, and why waves are safe |
| [fan-out.md](fan-out.md) | `.*` over a list, and joining the results back |
| [error-policy.md](error-policy.md) | `on_error: continue`, and what gets skipped |
| [when.md](when.md) | `when:`, and the one thing it's allowed to look at |
| [loop.md](loop.md) | `loop:`, pagination, and why it isn't fan-out |
| [nested-swarms.md](nested-swarms.md) | `swarm:`, nesting a whole swarm as one node |
| [hosting.md](hosting.md) | running it somewhere that is not your laptop |

## Connecting things

| Page | What it answers |
|---|---|
| [connections.md](connections.md) | every connection method, per provider, step by step |
| [connectors.md](connectors.md) | 1Claw's connector presets, and what they do not remove |
| [oneclaw-bridge.md](oneclaw-bridge.md) | vaults, agents, memory, posture, and the agent cap |
| [browser-bridge.md](browser-bridge.md) | driving an already-authenticated browser, and where it dead-ends |
| [llm.md](llm.md) | which model a bot actually talks to, and what it cost |
| [memory.md](memory.md) | what a bot remembers between runs |
| [harnesses.md](harnesses.md) | `bare` vs `openclaw`, and what a harness is not |

## Working on nanobots

| Page | What it answers |
|---|---|
| [testing.md](testing.md) | the full verification pass, and the claims that check themselves |
| [1claw-feature-requests.md](1claw-feature-requests.md) | what nanobots needs from 1Claw that does not exist yet |

`CLAUDE.md` at the repo root is the working style: what every change should
move, and the bugs that keep getting found.

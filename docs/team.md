# Team: a persistent, role-scoped coding agent

`context/TEAM-LAB-DESIGN.md` proposed a tier above the swarm model: `Human
-> Lab Agent -> Team agents -> nanobots and nanoswarms`. This page
documents that hierarchy's lower half — one Team harness (Claude Code or
Gemini CLI) against one role at a time. `docs/lab.md` documents the layer
above it: the chat surface that decides whether to delegate here at all.
The point of building Team this narrow first was to prove a Team agent can
do real work without a shortcut around the ordinary approval gate, before
anything got built on top of that assumption — which Lab now is.

## What it is

```sh
nanobots team run backend-engineer "add a stripe-watch bot to the catalog"
nanobots team run designer "..." --engine gemini
```

Gives one role one task, through one of two engines — Claude Code
(default) or Gemini CLI. Both were added the same way: verified against a
real invocation of the actual tool, not guessed from its docs — see
"What's verified" below. Under the hood:

- **The role's workspace is a real git worktree of this repo**, on its own
  branch (`team/<role>`), created once and reused on every later call —
  unlike the foundry's per-job worktree, which is thrown away after one
  task. A Team member's job is meant to accumulate history across many
  tasks, so its `git log` is part of what the next task's agent has to go
  on.
- **The Claude engine's sandbox is the foundry's, not a new one.** Same
  image (`harness/foundry-agent`, just the `claude` CLI, non-root, one
  writable mount), same container runner, same `--disallowedTools` defense
  in depth, same stream-json event parser. A Team member and a foundry job
  are the same kind of thing — a coding-agent CLI confined by a container
  boundary, given a git worktree — differing only in whether that worktree
  persists. See `internal/foundry/agent.go`'s package doc for why the
  container boundary, not a CLI permission flag, is what actually confines
  it.
- **The Gemini engine is a second, separate sandbox** (`harness/team-gemini`
  — same shape, a different npm package and entrypoint) rather than a
  detail bolted onto the first. Its permission model is coarser: Gemini
  CLI's `--approval-mode yolo` (auto-approve every tool call) rather than
  Claude's `acceptEdits` + `--disallowedTools`, because a real invocation
  showed `auto_edit` alone still stops for approval on non-edit tools with
  no TTY to answer it — headless needs `yolo` or nothing runs at all.
  `internal/team.ParseGeminiStreamJSONLine` is its own parser: Gemini's
  stream-json shape (`{"type":"tool_use","tool_name":...}`,
  `{"type":"message","role":"assistant","content":...,"delta":true}`) is
  close to Claude's in spirit and different in every field name.
- **It gets the same `nanobots` CLI a human would type**, built from the
  workspace's own source and mounted read-only, never a broader tool
  surface than that.

## The safety model, and why there's no separate gate to add

Team agents get **no elevated trust**. This was decided, not assumed —
`context/TEAM-LAB-DESIGN.md` names it as the property the rest of that
design depends on: a Team agent proposing a live action goes through
exactly the gate a human using the WebUI would hit, and never holds a
1Claw credential directly, for the same reason a bot never does.

Building this revealed that the gate doesn't need to be *added* — it
already exists for a very direct reason. A Team agent creates or edits
files in a git worktree of this repo, the same as a human editing YAML by
hand. Nothing in nanobots runs a swarm because its file changed on disk;
someone (or something) still has to trigger a run, and a run's write steps
still open an `approval_request` before anything real happens. A Team
agent authoring a new swarm is authoring a swarm — indistinguishable,
after the fact, from a human who typed the same YAML. The gate that
matters is the one nanobots already had.

What this build adds on top, as belt and suspenders: `--disallowedTools`
strips tools with no legitimate use in this context (scheduling cron jobs
on the host, messaging other agents, and so on — see
`foundry.DisallowedTools`, shared rather than duplicated so this list and
the foundry's can't drift apart), and the container never receives a 1Claw
key or any live credential — only its own separate `ANTHROPIC_API_KEY`.

## The one real, disclosed gap: its own credential

A Team member needs its engine's own key — `ANTHROPIC_API_KEY` for Claude,
`GEMINI_API_KEY` for Gemini — the same prerequisite the foundry's coding
agent already has. Not because Shroud lacks tool-calling — verified live,
it doesn't (`docs/1claw-feature-requests.md` #13) — but because a Team
engine is a real external CLI binary (`claude`, `gemini`) that speaks its
vendor's own API protocol directly, not a request
`internal/oneclaw.ShroudClient` ever builds; there's no proxy-shaped seam
in either CLI to route through Shroud even where Shroud itself could carry
the traffic. So token spend is metered by wall-clock and tool restrictions
on the sandbox itself, not a Shroud daily budget. See
`internal/foundry/job.go`'s package doc for the same trade-off, made once
and pointed to rather than re-argued here.

The key itself has two homes now, checked in this order: `~/.secrets/nanobots.env`
(unchanged — an existing local install needs nothing new), then a
`secrets.Store` entry (`anthropic/api_key` / `gemini/api_key`) paste-able
from Settings. The env file is local-only; a 1Claw Cloud Runtime
(`docs/oneclaw-bridge.md`) has no dotenv to edit, so before this, Team
delegation simply could not work on a cloud deployment — the daemon would
start, Lab would chat, and every delegation would fail on a missing key
with no way to supply one short of rebuilding the image. Settings' paste
path goes through whatever secrets backend actually resolved (a 1Claw
vault on a cloud runtime, a local encrypted file otherwise — see
[secrets.md](secrets.md)), so it works the same way in both places. It
still needs a restart to take effect: the key is read once at daemon
startup, same as every other credential this build reads at boot.

## Choosing which engine, live

Which engine a delegation actually uses used to be one field
(`lab.Config.DefaultEngine`), picked once at daemon startup from whichever
key was present — Claude preferred if both — with no way to see it or
change it short of editing the env file and restarting. Found live: a real
request silently ran on a Gemini free-tier key with a five-to-twenty-request
quota (`generativelanguage.googleapis.com/generate_content_free_tier_requests`)
and burned through it before failing, with nothing in the app saying it
would, or offering a way to pick differently.

Settings now shows both keys' status, a global default engine, and — since
a role is not a fixed catalog, only ever a directory `internal/team.Roles`
finds under `~/.nanobots/team/` for a role that has been delegated to at
least once — a per-role override for each one that exists, appearing the
first time Lab hands it a task. `team.Preferences` (`internal/team/preferences.go`)
holds this: a small JSON file next to the other state this daemon owns
(`~/.nanobots/state/team-engines.json`, the same override-file shape
`internal/roles.Store` already uses), and a pointer to the one live
instance is shared between the API's write handlers and Lab's own
`delegate()` call — so a change reaches the very next message, no restart,
unlike the key itself above.

`nanobots team run <role> "<task>" --engine gemini` still works exactly as
before — an explicit `--engine` flag on the CLI always wins over whatever
Settings has configured.

## What's verified, and what isn't yet

Verified, both engines: workspace creation and reuse (a role's worktree
persists across calls, survives its directory being removed by hand as
long as the branch does, and two roles never see each other's files), the
CLI's argument handling, and that a missing-credential path fails before
touching git at all — confirmed by running `nanobots team run
backend-engineer "..."` with no key configured and checking `git worktree
list` / `git branch` came back clean afterward.

**Verified end to end, for real, with Gemini:**

```
$ nanobots team run designer "Read docs/anatomy.md and reply with one
  sentence describing what a nanoswarm is. Do not write or edit any file."
  --engine gemini

designer: preparing workspace (team/designer), engine gemini…
[tool] read_file docs/anatomy.md
[text] A nanoswarm is a declarative, YAML-configured automation workflow that
[text] specifies a set of nanobots to execute under designated trigger
       conditions and wires their output ports to input ports using
       type-checked connections called snaps.

I made no changes to any files, and no work is left pending.
[result] done
```

A real container, a real file read, an accurate answer grounded in the
actual doc content, the instruction not to edit anything honored, and
`git status` in the resulting worktree came back clean afterward. This is
the proof the design doc's own "Proposed next step" asked for.

**Not yet verified with real credit: Claude.** The only `ANTHROPIC_API_KEY`
available when this was tested belongs to a different project's account
with no credit balance — the run reached the real Anthropic API (proof the
container, the image, and the key passthrough all work) and failed there
with `Credit balance is too low`, not with a bug in this build. Mechanically
proven, not yet proven doing real work. `TestTeamRunGivesARealTaskToARealAgent`
(`internal/team/run_test.go`) covers both engines, skipped by the same
`NANOBOTS_LIVE_TEST=1` convention `agent_claude_live_test.go` uses; running
it against a funded Anthropic key is the honest remaining step for that
engine specifically.

## What's deliberately not built yet

- **No cross-agent memory.** The design doc's Honcho-based
  "recall-before-work, remember-after-work" pattern (step 3 of its
  "Proposed next step") isn't wired in — this is one role, one task at a
  time, with no coordination story yet for two Team agents working the
  same company.
- **A third coding-agent CLI is unbuilt, not a second.** Gemini CLI is now
  in (`internal/team.EngineGemini`), verified the same way Claude was —
  against a real invocation, not its docs. OpenCode, Hermes, OpenClaude,
  and plain `claude-code`/`opencode`/`openclaude` bot-harness names are
  still rejected by `internal/runner` (`docs/harnesses.md`) — a different
  thing from a Team engine, since a bot's harness runs a fixed step list
  and a Team engine runs an open-ended coding session, but the same
  "verify against a real invocation first" discipline applies to whichever
  gets built next.
- **Not the second `foundry.Agent` its own doc comment anticipated.**
  `internal/foundry/agent.go` says a second coding-agent CLI is "meant to
  be a second implementation of this same interface" — `Agent.Run(ctx,
  workDir, BriefInput, events)`, shaped around "author one new bot." Team's
  Gemini engine doesn't implement it; `internal/team.Run` calls
  `foundry.RunDockerAgent` directly with its own args and its own parser,
  because a Team task isn't "author one new bot" and forcing it through
  `BriefInput` would have meant stretching that shape to fit a job it
  wasn't built for. `EnsureClaudeCodeImage`, `DockerAgentSpec`,
  `RunDockerAgent`, `DisallowedTools` and `BuildScopedBinary` are what
  actually got reused — the container-running primitives, not the
  bot-authoring interface sitting in front of them.
- **No multi-tenancy.** One nanobots install, one team, matching this
  build's existing single-tenant shape everywhere else. The design doc's
  "multiple companies" question is still open.

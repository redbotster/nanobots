# Team: a persistent, role-scoped coding agent

`context/TEAM-LAB-DESIGN.md` proposed a tier above the swarm model: `Human
-> Lab Agent -> Team agents -> nanobots and nanoswarms`. This page is that
design's own "Proposed next step," item 2 — exactly one Team harness
(Claude Code), against exactly one role at a time, with no Lab tab yet.
The point of building it this narrow first is to prove a Team agent can do
real work without a shortcut around the ordinary approval gate, before
anything gets built on top of that assumption.

## What it is

```sh
nanobots team run backend-engineer "add a stripe-watch bot to the catalog"
```

Gives one role one task. Under the hood:

- **The role's workspace is a real git worktree of this repo**, on its own
  branch (`team/<role>`), created once and reused on every later call —
  unlike the foundry's per-job worktree, which is thrown away after one
  task. A Team member's job is meant to accumulate history across many
  tasks, so its `git log` is part of what the next task's agent has to go
  on.
- **The sandbox is the foundry's, not a new one.** Same image
  (`harness/foundry-agent`, just the `claude` CLI, non-root, one writable
  mount), same container runner, same `--disallowedTools` defense in depth,
  same stream-json event parser. A Team member and a foundry job are the
  same kind of thing — a coding-agent CLI confined by a container boundary,
  given a git worktree — differing only in whether that worktree persists.
  See `internal/foundry/agent.go`'s package doc for why the container
  boundary, not a CLI permission flag, is what actually confines it.
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

A Team member needs `ANTHROPIC_API_KEY` in `~/.secrets/nanobots.env` — the
exact same prerequisite the foundry's coding agent already has, and the
exact same reason: `internal/oneclaw.ShroudClient.Chat` is a single-shot,
one-message-in/one-message-out proxy, not a multi-turn, tool-using session
an external CLI agent could sit behind. There is no way to route this
through 1Claw/Shroud today, so its token spend is metered by wall-clock and
tool restrictions on the sandbox itself, not a Shroud daily budget. See
`internal/foundry/job.go`'s package doc for the same trade-off, made once
and pointed to rather than re-argued here.

## What's verified, and what isn't yet

Verified: workspace creation and reuse (a role's worktree persists across
calls, survives its directory being removed by hand as long as the branch
does, and two roles never see each other's files), the CLI's argument
handling, and that the missing-credential path fails before touching git
at all — confirmed by running `nanobots team run backend-engineer "..."`
with no key configured and checking `git worktree list` / `git branch`
came back clean afterward.

**Not yet verified: a real run.** This build's own `~/.secrets/nanobots.env`
has no `ANTHROPIC_API_KEY` configured, so the actual Claude Code session —
the part that matters most, whether a Team agent proposing a real change
behaves the way the brief says it should — has not been run end to end.
`TestTeamRunGivesARealTaskToARealAgent` (`internal/team/run_test.go`) is
written and skipped by the same `NANOBOTS_LIVE_TEST=1` convention
`agent_claude_live_test.go` uses; it needs a real key to actually prove
anything, and running it is the honest next step before trusting this at
all, let alone building Lab or a second role on top of it.

## What's deliberately not built yet

- **No Lab tab.** `context/TEAM-LAB-DESIGN.md`'s own sequencing holds:
  design Lab's chat surface once this and the recall-before-work step
  below have actually run, informed by what they required rather than
  guessed in advance.
- **No cross-agent memory.** The design doc's Honcho-based
  "recall-before-work, remember-after-work" pattern (step 3 of its
  "Proposed next step") isn't wired in — this is one role, one task at a
  time, with no coordination story yet for two Team agents working the
  same company.
- **No second coding-agent CLI.** OpenCode, Hermes, OpenClaude, and plain
  `claude-code`/`opencode`/`openclaude` harness names are still rejected by
  `internal/runner` (`docs/harnesses.md`). `internal/foundry.Agent` was
  already designed as an interface a second adapter could implement the
  same way `ClaudeCLIAgent` was — verified against a real invocation of
  that tool, not guessed from its docs — and that applies here too.
- **No multi-tenancy.** One nanobots install, one team, matching this
  build's existing single-tenant shape everywhere else. The design doc's
  "multiple companies" question is still open.

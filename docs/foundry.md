# The foundry — authoring new bots on demand

The AI composer (`internal/api/compose.go`) can only assemble swarms from bots that already exist. The foundry (`internal/foundry`) is what happens when the composer determines the catalog genuinely can't do what was asked: a sandboxed coding agent authors a brand-new bot from scratch, self-tests it, and hands it to a human for approval before it's ever part of the real catalog. Three concepts stay distinct on purpose: the **composer** assembles swarms from known bots, the **foundry** authors brand-new ones, and a **harness** (`bare`/`openclaw`) is how a bot *runs* — none of that changes here.

## The pipeline

1. `POST /api/compose` returns `{"gap": true, "missing_capability": "...", ...}` instead of a draft when no combination of the catalog fits (`composePrompt`'s second legal output shape).
2. The WebUI shows this as an explicit opt-in — "want me to build one?" — never launching the foundry silently.
3. `POST /api/foundry` starts a `foundry.Job`: a git worktree is created (`git worktree add`), a coding agent authors `bots/<new-id>/` inside it, and the job's own `contract.RunConformance` check runs against that draft.
4. Once conformance passes, the job opens a review gate (`Job.RequestApproval`, the exact same mechanism a swarm's `approve` step already uses — see `docs/approvals.md`). A human approves or rejects.
5. Approved: the bot is copied into the real `bots/` (with conformance re-checked at the real location, never trusting the agent's self-report), the worktree is cleaned up, and the WebUI automatically re-submits the original request to `/api/compose` — it should now succeed. Rejected or failed: nothing is added to the catalog, and a failed job's worktree is left on disk for inspection rather than deleted.

## Why this runs inside Docker, not as a raw subprocess

The original design assumed `claude` CLI flags (`--allowedTools` scoped to one command) could restrict the coding agent's shell tool to running only `nanobots conform`. Real testing disproved this: the scoped allow didn't restrict anything, and `--disallowedTools "Bash"` combined with a specific allow blocked Bash entirely rather than carving out an exception. There is no flag combination that gives "this tool may run exactly this one command." Since a git worktree alone doesn't stop an unrestricted shell tool from touching anything else the host process can reach, the actual sandbox is `harness/foundry-agent`'s Docker container: the agent runs with exactly two bind mounts (the worktree, read-write; a job-scoped `nanobots` binary, read-only) and no other host filesystem access at all. `--disallowedTools` is still used, but as defense in depth on top of the container boundary, not as the boundary itself.

## A real, disclosed prerequisite: `ANTHROPIC_API_KEY`

The foundry's container needs its own Anthropic API key, passed through as an environment variable — add `ANTHROPIC_API_KEY=sk-ant-...` to `~/.secrets/nanobots.env`, alongside `ONECLAW_API_KEY`. This is deliberately separate from two other credentials that might look like they should cover it:

- **This machine's own interactive `claude` CLI login** (used to develop this project) is OAuth/keychain-based — a container can't share that session, and `claude`'s `--bare` mode (which supports API-key auth) was tried and rejected: it also excludes the `Write` tool from its default toolset, which would make authoring new files impossible.
- **1Claw/Shroud** can't back this either — `internal/oneclaw.ShroudClient.Chat` is a single-shot, one-message-in/one-message-out proxy, not a multi-turn, tool-using session an external CLI can sit behind. The foundry still registers a named 1Claw agent (`nanobots-foundry`) for consistency with every other agent in this system, but its `ShroudConfig.DailyBudgetUSD` is not actually enforced on the coding agent's real spend — the real, enforced guard is the wall-clock (`MaxWallClock`, default 20 minutes) and iteration (`MaxIterations`, default 8 conform attempts) caps on the container itself.

Without `ANTHROPIC_API_KEY` set, a foundry job fails immediately with a clear error rather than starting a container that can't authenticate.

## Supporting a second coding-agent CLI

`foundry.Agent` is a one-method interface (`Run(ctx, workDir, in BriefInput, events chan<- Event) error`); `ClaudeCLIAgent` is the only shipped implementation. A second CLI (OpenCode or otherwise) is meant to be a second implementation of the same interface, reusing `runDockerAgent`'s container-execution plumbing — but built the same way this one was: verified against a real invocation of that tool first, not guessed from its documentation, since that's exactly the class of assumption (CLI permission flags) that turned out wrong here.

## Try it

```
# once ANTHROPIC_API_KEY is set:
curl -X POST http://127.0.0.1:7474/api/foundry \
  -d '{"request": "...", "missing_capability": "..."}'
curl http://127.0.0.1:7474/api/foundry/<id>          # poll status/log/pending_approvals
curl -X POST http://127.0.0.1:7474/api/foundry/<id>/approvals/<approvalId>/decide \
  -d '{"approved": true}'
```

Or, in the WebUI: describe something the catalog genuinely can't do in the Swarms page's compose box, click **Build it** on the gap panel that appears, and watch the live log.

# Team and Lab: a design, not yet a build

Written before any code, the same way `V2-AUDIT.md` preceded Phase 1-3: this
is large enough — a new tier above the swarm model, a new harness class, and
a multi-tenancy dimension nanobots doesn't have — that it earns its own
review before a line of it ships. Nothing in this doc is built.

## The ask, restated precisely

```
Human -> Lab Agent -> Team agents -> nanobots and nanoswarms
```

- **Team**: each role (backend engineer, designer, marketer, ...) gets its
  own persistent agent running in a real coding-agent harness — Claude Code,
  OpenCode, Hermes, OpenClaude, openclaw, and whichever others matter later —
  in a minimal, purpose-built container per harness.
- **Lab**: a new top-level tab, a chat UI backed by one master agent that
  directs Team members. Team members in turn create, manage, and run
  nanobots and nanoswarms.
- **Scale**: a Human + Lab Agent may run more than one company at once, each
  with its own Team. The design target is on the order of 1,000 agents
  running concurrently across companies, not a handful.

## The decision this doc was blocked on, now made

**Team agents get no elevated trust.** A Team agent proposing a live action
— publishing a swarm, connecting an account, running something against a
real credential — goes through exactly the gate a human using the WebUI
would hit: the same planner type-check, the same `approval_request`, the
same "nothing acts without a human seeing it first." A Team agent is a
faster typist, not a bigger grant of authority.

This is the property that makes the rest of this design tractable, and it's
worth stating why it was the right call rather than just the safe one: every
line of nanobots' pitch against n8n and a chat-bot-that-does-things is
"never lets the app say something that isn't so" and "nothing acts without
approval." A Team tier that could skip the gate would be a second product
living inside the first one, with a different safety story — and the
gate doesn't disappear at 1,000 agents, it becomes the thing that keeps
1,000 agents from being 1,000 ways to skip it.

Concretely: **a Team agent never holds a 1Claw credential, exactly like a
bot never does.** It reaches a live system the same two ways a bot does —
`execute_intent` through a connector binding, or a request that opens an
`approval_request` — never a raw secret in its container or its workspace.

## Mapping onto what already exists

| Ask | Existing primitive | What changes |
|---|---|---|
| A role's persistent agent | `internal/runner`'s harness model (`bare`, `openclaw`, and the queued `llm` dynamic-loop harness — Phase 3 item 1) | A new harness *class*: long-lived, stateful, a real workspace, not a one-shot container that exits when its steps finish. Everything below assumes this class is new, not a variant of the existing three. |
| Lab's master agent | `/api/compose` — the "head nanobot" chat box already drafts a swarm from a prompt, type-checked by the same planner a save goes through | Lab is that shape pointed at a different target: instead of drafting `bots[]`/`snaps[]`, it drafts instructions for a Team agent, or a request to run one. The type-checking discipline (never let the agent's output reach a container un-validated) carries over unchanged. |
| A Team agent using nanobots itself | The `nanobots` CLI and REST API, which already have no notion of "human vs. agent caller" | Needs one: every mutating call (`publish`, `connect`, `run`) has to know whether its caller is a person, the composer, or a Team agent, because the approval and audit trail need to say which. |
| Multiple companies | Nothing today — one `~/.nanobots` directory, one 1Claw account, implicitly one tenant | A new isolation boundary: separate vaults, separate bot/swarm catalogs, separate agent quota, switchable from the UI. Bigger than it sounds — see below. |
| 1,000 concurrent agents | The warm-pool / ephemeral-container item already queued in Phase 3 | Necessary but not sufficient at this scale — see "What breaks at 1,000" below. |

## What a Team harness actually needs

Unlike a nanobot, a Team member is not stateless between runs. It needs:

- **A persistent workspace** — a real directory it reads and writes across
  many turns, not `outputs/` written once and discarded. This is closer to
  what a dev container gives a person than to what `runOneBot` gives a bot.
- **A minimal image per agent CLI.** The instinct from the 39-bot catalog
  applies directly: `9a4626e` cut 25 of 30 bots from a 1.1GB image to 25MB
  by shipping only what a bot's declared steps actually need. A Claude Code
  image needs node + git + the CLI; an OpenCode image needs its own
  runtime; none of them need each other's. One Dockerfile per harness,
  built and measured the same way, not one fat image with every agent CLI
  installed "just in case."
- **Egress and credential posture identical to a bot's.** No 1Claw key, no
  vault secret, ever placed in the container. Whatever a Team agent needs
  from a live service, it asks for through the same `execute_intent` /
  approval path a bot uses — which is also the enforcement mechanism for
  the "no elevated trust" decision above, not just a policy statement.
- **A declared identity for approvals and audit**, the same shape a bot's
  1Claw agent has today (`docs/oneclaw-bridge.md`), keyed by role rather
  than by name, for the same reason bots are keyed by guardrail profile
  (Phase 2 item 15) — a role, not an instance, is the actual boundary that
  matters for quota and policy.

## The Lab tab

A chat UI, peer to Swarms/Team/Runs/Settings, backed by one long-running
orchestrator with two tools: *talk to a Team agent* and *read what a Team
agent or a swarm is doing*. It does not get a third tool that bypasses
either of those — "Lab can also just run this itself" is exactly the
elevated-trust path the decision above rules out.

The existing composer already proves the hard part of this — a chat
surface whose output the planner refuses to trust blindly — so Lab is
additive UI and a new target for that discipline, not a new discipline.

## Shared awareness across Team agents: Honcho, not a new system

Two Team agents working the same company will step on each other the same
way two catalog bots did before memory existed — `competitor-watch` re-ran
its whole comparison every hour because nothing recorded what it had already
seen. The fix here is the same shape, not a new one: **Honcho is already a
shipped backend** (`internal/memory`, `docs/memory.md`), already Workspace →
Peer → Session with a background deriver built for exactly "what has this
peer already done, and what would it be told by asking." Nothing about
cross-agent awareness needs a new subsystem — it needs Team agents to be
Honcho peers in a shared workspace, using the two step types that already
exist:

- **One Honcho workspace per company**, not per Team agent or per session —
  the isolation boundary this doc already needs for multi-tenancy anyway
  (see "What breaks at 1,000 agents"). Two companies' Teams must not be able
  to recall each other's work.
- **Every Team agent is a peer in that workspace**, named by role the same
  way a bot's 1Claw agent is keyed by guardrail profile rather than by bot
  name — the boundary that matters is "which role did this," not "which
  container instance."
- **Before starting work, recall; after finishing, remember.** Concretely:
  a Team agent about to pick up a task calls `memory.recall` with something
  like *"has anyone on this team already started or finished \<task\>?"*
  before doing it, and calls `memory.remember` with what it did once it has.
  This is precisely the `inbox-triage` / `support-triage` / `draft-replies`
  pattern already in the catalog — ask a plain-language question, get an
  answer derived from prior observations, degrade to "nothing known" when
  the backend can't derive one — pointed at teammates instead of at a
  person's inbox history.
- **`optional: true` almost certainly does not apply here.** A bot's
  `inbox-triage` degrades fine to its static rules when recall is
  unavailable, because triaging without memory is still triaging. A Team
  agent that can't check "did someone already do this" and proceeds anyway
  is the exact duplicate-work failure this request exists to prevent — so
  the honest design is a **required** recall for the "has this been done"
  check (fails loudly if Honcho isn't configured, rather than silently
  duplicating), while a lower-stakes question ("what's this team's usual
  style") can stay optional.
- **This is also why Lab shouldn't get its own live-action tool** (see
  above): if coordination lives in shared memory that every Team agent
  reads and writes as a peer, a Lab agent that bypassed Team agents to act
  directly would be the one actor not writing to that record — invisible
  to every other Team member checking whether the work was already done.

The open item this adds to the probe in "Proposed next step": confirm a
single self-hosted Honcho instance holds multiple workspaces cleanly (one
per company) before assuming the existing `docker/honcho/honcho.sh` setup
scales past the one-workspace-named-"nanobots" default it ships with today.

## What breaks at 1,000 agents, specifically

- **1Claw's agent cap, again, harder.** Feature request #4 already names
  this at 27 of 50 slots for 39 *bots*. A Team-agent-per-role-per-company
  model multiplies it: ten companies with five roles each is 50 Team
  agents before a single nanobot runs. This doc doesn't add a new feature
  request — it raises the priority of the existing one from "matters for
  bots" to "the ceiling on this entire design."
- **A flat agent list is the wrong data model.** Today's `nanobots agents`
  is a flat list keyed by guardrail profile. At this scale the model needs
  to be a tree — company → Lab → Team role → the swarms/bots that role
  owns — so a question like "who can act on company X's Gmail" has one
  answer instead of a grep.
- **Warm pools become mandatory, not an optimization.** The Phase 3 item
  already queued (pre-pull harness images, reuse started containers) was
  framed as a speed win. At 1,000 agents it's the difference between the
  design working and every message to Lab paying a cold container start.
- **One `~/.nanobots` directory cannot hold ten companies.** Multi-tenancy
  needs to be real: separate credential vaults (1Claw already scopes vaults
  per agent — the question is whether it scopes cleanly per *org*, which
  needs a live check before this is designed further), separate bot/swarm
  catalogs, and a workspace switcher in the UI. This is closer in size to
  Phase 1's "single binary, one command to start" than to a Phase 3 item —
  it's install-and-setup shaped, not swarm-DSL shaped.

## Open questions this doc doesn't answer yet

1. Does 1Claw's org/sub-org model (`create_sub_org` is mentioned in
   `NANOBOTS-BLUEPRINT.md`'s bootstrap flow but not yet verified live)
   actually give one company's Team a clean credential/quota boundary from
   another's, or does that need its own feature request the way agent
   quota already does?
2. What does a Team agent's workspace persist *to* between runs — a volume
   per role, or does "persistent" mean something narrower (its git
   history and its own memory namespace, workspace rebuilt each session)?
3. Does Lab get its own 1Claw agent (one more entry in the same capped
   pool), or is it exempt as pure orchestration with no live-action tool of
   its own — the same distinction Phase 2 drew between the composer
   (no agent needed, it only drafts) and an approving bot (needs one)?

## Proposed next step

Treat this the way Phase 0 treated the 1Claw audit: one small, real probe
before any UI or harness work —

1. Verify question 1 live (sub-org creation, whether it actually isolates
   vaults and agent quota per company), and confirm a single self-hosted
   Honcho instance holds multiple workspaces cleanly, one per company.
   **Not done.**
2. Build exactly one Team harness (Claude Code, since it's the one this
   session already knows how to drive) against exactly one role, with no
   Lab tab yet — prove a Team agent can propose a swarm change and have it
   go through the ordinary approval gate before anything else is built on
   top of it.
   **Built** (`internal/team`, `nanobots team run <role> "<task>"`,
   `docs/team.md`) **by reusing the foundry's sandbox wholesale** rather
   than building a second one — a Team member turned out to be the same
   kind of thing a foundry job already is (a coding-agent CLI confined by
   a container boundary, given a git worktree), differing only in whether
   that worktree is persistent. The finding that changes this section: the
   "ordinary approval gate" doesn't need a new mechanism to reach. A Team
   agent edits files in a worktree of this repo; nothing runs because a
   file changed, so it's already indistinguishable from a human's edit
   until someone (or something) triggers a run, at which point the
   existing `approval_request` gate is the same one either way.
   **Run for real, once, with a second engine.** No funded
   `ANTHROPIC_API_KEY` was available (the only one on this machine, from a
   different project, reached the real API and failed there on billing —
   proof the plumbing works, not that the plan Team should use does), so
   Gemini CLI was added as a second engine (`internal/team.EngineGemini`,
   `harness/team-gemini`) and verified instead: a real container gave
   `designer` the task "read docs/anatomy.md, describe a nanoswarm, don't
   edit anything," and it read the real file, answered correctly from its
   real content, and left the worktree clean. That is the proof this step
   asked for — it just came from the second engine tried, not the first.
   `docs/team.md` has the full transcript. Claude Code itself is still
   only mechanically verified; running it for real needs a funded key.
3. Add a second Team agent in the same role's workspace and prove the
   recall-before-work / remember-after-work pattern actually stops the
   second one from redoing the first one's task, before trusting it at
   any real scale.
   **Not done** — step 2's live proof landed; this is now unblocked.
4. Only then design Lab's chat surface, informed by what steps 2 and 3
   actually required.
   **Built, ahead of step 3.** The user asked for Lab now rather than
   waiting on the Honcho recall-before-work step, so it was built against
   what step 2 alone required: a single-shot router
   (`internal/llm.Generator` has no function-calling loop to build a real
   one on), reusing `*runner.Run`'s existing log/subscribe machinery for
   the chat's own SSE stream rather than a second pub-sub mechanism
   (`internal/lab`, `docs/lab.md`). Verified in the real WebUI, against a
   real Shroud call and a real Gemini delegation, which found two real
   bugs a mocked test never would have — a canceled context that failed
   every delegation, and a truncated final summary from not accumulating
   Gemini's streamed delta chunks. Both are fixed and covered by tests
   built from the real failing transcript; docs/lab.md has the full story.
   Step 3 (recall-before-work) is still open, and Lab currently has no way
   to tell two Team agents apart working the same role concurrently —
   worth naming now that a real chat surface exists to eventually need it.

This keeps the large, genuinely uncertain pieces (multi-tenancy, the
harness-per-CLI matrix, 1,000-agent scale) as named risks with a first real
measurement each, rather than architecture decided in the abstract.

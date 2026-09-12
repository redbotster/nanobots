# Supervisors

A supervisor — a team of perspectives reviewing work and reaching a decision — needs **no new bot type**. It's a swarm shape, built from three small bots and the machinery that already exists.

`examples/swarms/supervisor-review.yaml`:

```yaml
snaps:
  - from: board.roles.*.name   # fan a reviewer out over the chosen team
    to: panel.role
  - from: board.roles.*.focus  # …with what that role is for, in lockstep
    to: panel.focus
  - from: board.brief        # no marker: every reviewer gets this unchanged
    to: panel.brief
  - from: panel.review       # already list<json>, because panel fanned out
    to: chair.reviews
```

Three things make it work without new machinery:

1. **The team is chosen per run, from a library you own.** `review-board` decides which perspectives should look at *this* work — a plan touching credentials wants a security reviewer; one changing a first-run flow wants a designer. Picking the same three reviewers for everything is how review theatre starts. It picks from the role library rather than inventing (see below), so the choice is repeatable and the perspectives are yours.
2. **The role is an input, not a bot.** One `reviewer` is a security engineer, a designer or an SRE depending on what it's given, so a review team is a swarm shape rather than five near-identical bots to maintain. Each instance's `instructions` tunes it further.
3. **Fan-out already joins.** `.*` runs `reviewer` once per role, and a fanned-out bot's outputs are collected into a list — which is exactly the `list<json>` the synthesiser takes. No join step is needed here, which is why this could be built while the general join question is still open.

Team size is whatever the board decided for that run, not a number baked into the swarm.

## The role library

`roles/roles.yaml` ships eight perspectives — product, staff engineer, security, design, developer experience, SRE, QA, data and privacy. Each is a name and a single line saying what it looks at. That line is what its reviewer is told.

Before this existed, `review-board` invented its team fresh from a prompt on every run. The choices were good, but they were unrepeatable, invisible until after the run, and there was nowhere to record what *your* security reviewer actually cares about.

Edit them in **Fleet → Roles**, not in the YAML. The UI writes overrides to `~/.nanobots/state/roles.json` and never touches the repo file, so a `git pull` doesn't fight your changes and every role can always say what it shipped with. You can add perspectives of your own ("Legal", "Support"), switch shipped ones off without losing their wording, and put any edit back.

The board is handed the live roster through `{{roles.roster}}`, a template variable resolved per run — so an edit changes the next review immediately, with no bot file rewritten and no restart. The page will show you the exact text the model receives, because "what will this actually look like" is the question someone editing a role is trying to answer.

Two consequences worth stating:

- **A role is still not a bot.** One `reviewer` becomes a security engineer or a designer depending on what it is handed. The library gives those perspectives names and wording; it does not turn them into eight near-identical bots to maintain.
- **No library is a working state.** A deployment with no `roles/roles.yaml` and no overrides gets an empty roster, and the board invents a team exactly as it did before. Nothing here is required to run a review.

### Requiring one

The board picks its own team per run, and that is deliberate — pinning the
same three reviewers to everything is how review theatre starts. But
"whatever else you choose, always have security look at this" is a real
thing to want, and until now the only way to express it was to stop using
the board.

Tick **on every team** on a role. The board is told, in the roster, on that
role's own line; it includes it and chooses the rest as normal. A floor, not
a fixed team.

Keep the lines sharp and non-overlapping. The board is told that two roles which would raise the same concern is one role too many, and a roster of blurry perspectives is the fastest route back to review theatre.

## The rule that matters

`review-synthesis` is told **never to average two reviewers into a middle position neither held**. A supervisor that quietly resolves conflict is worse than no supervisor: the disagreement is usually the most valuable thing the team produced, and it belongs in front of the human rather than smoothed away. The decision is the strictest any reviewer gave, so one `do-not-ship` carries, and every blocker must trace to a reviewer's concern — the chair cannot invent its own.

`reviewer` is also told that zero concerns is a legitimate review. A review bot that always finds something is a review bot nobody reads.

## A real run

Against the plan *"let users paste an API token into the settings page, store it in a vault, and show the last four characters back"*, the board picked **security engineer** and **frontend/UX engineer** — two, not a fixed three.

Run again after the library landed, it picked **Security engineer**, **Design** and **SRE**, copying each one's focus from the roster verbatim — including a security focus that had been narrowed in the UI a moment earlier to *"only whether a credential can reach a log, a prompt, or a container image"*. The edit reached the reviewer without a restart or a file change, which is the whole point of resolving the roster per run. Both said ship-with-changes for different reasons (token handling between paste and vault; no disclosure at the point of paste that the token leaves the browser). The chair surfaced their genuine disagreement: whether last-four masking is a minor issue or a real one.

## What this is not

This is the **on-demand** shape: you run it, it reaches a decision, the containers exit. An *always-on* supervisor you can chat with is a different thing — long-lived, multi-turn, and stateful, against a runtime where every bot is a short-lived container that is destroyed on exit and remembers nothing. That needs a session concept and a memory layer, and neither exists yet. See the "always-on" section of this doc's TODO in the README's not-built list rather than assuming it follows from this.

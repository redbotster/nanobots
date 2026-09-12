# Supervisors

A supervisor — a team of perspectives reviewing work and reaching a decision — needs **no new bot type**. It's a swarm shape, built from three small bots and the machinery that already exists.

`examples/swarms/supervisor-review.yaml`:

```yaml
snaps:
  - from: board.roles.*      # fan a reviewer out over the chosen team
    to: panel.role
  - from: board.brief        # no marker: every reviewer gets this unchanged
    to: panel.brief
  - from: panel.review       # already list<json>, because panel fanned out
    to: chair.reviews
```

Three things make it work without new machinery:

1. **The team is chosen, not configured.** `review-board` decides which perspectives should look at *this* work. That's a judgement — a plan touching credentials wants a security reviewer; one changing a first-run flow wants a designer. Picking the same three reviewers for everything is how review theatre starts.
2. **The role is an input, not a bot.** One `reviewer` is a security engineer, a designer or an SRE depending on what it's given, so a review team is a swarm shape rather than five near-identical bots to maintain. Each instance's `instructions` tunes it further.
3. **Fan-out already joins.** `.*` runs `reviewer` once per role, and a fanned-out bot's outputs are collected into a list — which is exactly the `list<json>` the synthesiser takes. No join step is needed here, which is why this could be built while the general join question is still open.

Team size is whatever the board decided for that run, not a number baked into the swarm.

## The rule that matters

`review-synthesis` is told **never to average two reviewers into a middle position neither held**. A supervisor that quietly resolves conflict is worse than no supervisor: the disagreement is usually the most valuable thing the team produced, and it belongs in front of the human rather than smoothed away. The decision is the strictest any reviewer gave, so one `do-not-ship` carries, and every blocker must trace to a reviewer's concern — the chair cannot invent its own.

`reviewer` is also told that zero concerns is a legitimate review. A review bot that always finds something is a review bot nobody reads.

## A real run

Against the plan *"let users paste an API token into the settings page, store it in a vault, and show the last four characters back"*, the board picked **security engineer** and **frontend/UX engineer** — two, not a fixed three. Both said ship-with-changes for different reasons (token handling between paste and vault; no disclosure at the point of paste that the token leaves the browser). The chair surfaced their genuine disagreement: whether last-four masking is a minor issue or a real one.

## What this is not

This is the **on-demand** shape: you run it, it reaches a decision, the containers exit. An *always-on* supervisor you can chat with is a different thing — long-lived, multi-turn, and stateful, against a runtime where every bot is a short-lived container that is destroyed on exit and remembers nothing. That needs a session concept and a memory layer, and neither exists yet. See the "always-on" section of this doc's TODO in the README's not-built list rather than assuming it follows from this.

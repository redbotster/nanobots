# review-board

Given a piece of work, decide which perspectives should review it, and write one shared brief.

This is the first half of a supervisor: choosing the team *is* a judgement, not configuration. A plan that touches credentials wants a security reviewer; a plan that changes a first-run flow wants a designer. Picking the same three reviewers for everything is how review theatre starts.

`roles` is a `list<string>`, so a swarm fans `reviewer` out over it with a `.*` snap (see `docs/fan-out.md`) — the team size is whatever this bot decided, not a number baked into the swarm.

It reads nothing and writes nothing: one model call, two outputs. Change how it picks by editing its `instructions`, on its card or per swarm.

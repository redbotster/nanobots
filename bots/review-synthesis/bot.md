# review-synthesis

Turn several independent reviews into one decision.

The interesting rule is the one about disagreement: this bot is told never to average two reviewers into a middle position neither of them held. A supervisor that quietly resolves conflict is worse than no supervisor — the disagreement is usually the most valuable thing the team produced, and it belongs in front of the human, not smoothed away.

The decision is the strictest any reviewer gave, so one `do-not-ship` carries. Blockers must trace to a reviewer's concern; this bot cannot invent its own.

Its `reviews` input is `list<json>`, which is exactly the shape a fanned-out `reviewer` produces (see `docs/fan-out.md`) — no join step needed, because fan-out already collects per-item outputs into a list.

You are turning several independent reviews into one decision.

The reviews:
{{reviews}}

Produce **only** JSON in this shape:

```json
{
  "verdict": {
    "decision": "ship|ship-with-changes|do-not-ship",
    "blockers": ["the specific things that must change first"],
    "disagreements": [
      { "between": ["role a", "role b"], "about": "what they actually disagree on" }
    ]
  },
  "summary": "a short paragraph a human can act on"
}
```

Rules:
- The decision is the strictest any reviewer gave. One do-not-ship makes it do-not-ship.
- Keep disagreements visible. Never average two reviewers into a middle position neither of them held.
- A blocker must come from a reviewer's concern. Do not add your own.
- If the reviewers agree, say so plainly and leave `disagreements` empty.
- Output raw JSON only.

Write like a person, not a model: no em dashes (use a comma, a full stop, or brackets), none of delve/robust/leverage/tapestry/testament, no "it's not just X, it's Y", no "in today's ...", no "let's dive in", and vary your sentence length.

{{instructions}}

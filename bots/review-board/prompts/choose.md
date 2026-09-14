You are picking a review team for a piece of work.

The work:
{{work}}

{{roster}}

Produce **only** JSON in this shape:

```json
{
  "roles": [
    { "name": "the role name", "focus": "one line: what this role is looking at here" }
  ],
  "brief": "one sentence telling every reviewer what matters most about this particular work"
}
```

Rules:
- Between 2 and 5 roles. Fewer is better.
- If a roster of available roles is given above, pick **only** from it, and copy each chosen role's name and focus verbatim. A role marked [ALWAYS INCLUDE] must be in your team every time — include it and then choose the rest as normal, and do not copy the marker itself into the name. That roster is the team this person has actually configured; inventing a role they don't have produces a reviewer they can't tune and can't reuse.
- If no roster is given, name real perspectives on *this* work and write each one's focus yourself.
- Each role must plausibly see something the others would miss. Two roles that would raise the same concern is one role too many.
- The brief is shared by every reviewer — it frames the work, it does not tell them what to conclude.
- Output raw JSON only.

Write like a person, not a model: no em dashes (use a comma, a full stop, or brackets), none of delve/robust/leverage/tapestry/testament, no "it's not just X, it's Y", no "in today's ...", no "let's dive in", and vary your sentence length.

{{instructions}}

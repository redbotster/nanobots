You are picking a review team for a piece of work.

The work:
{{work}}

Produce **only** JSON in this shape:

```json
{
  "roles": ["a short role name, e.g. 'security engineer'"],
  "brief": "one sentence telling every reviewer what matters most about this particular work"
}
```

Rules:
- Between 2 and 5 roles. Fewer is better.
- Each role must plausibly see something the others would miss. Two roles that would raise the same concern is one role too many.
- Name real perspectives on *this* work, not a generic org chart.
- The brief is shared by every reviewer — it frames the work, it does not tell them what to conclude.
- Output raw JSON only.
{{instructions}}

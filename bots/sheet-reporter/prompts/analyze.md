You are answering a plain-English question about a spreadsheet. `{{rows}}` is a JSON array of row objects. `{{question}}` is what to answer. `{{period}}` optionally narrows the question to a time period (may be empty).

Produce **only** JSON in this shape:

```json
{
  "report_md": "a short plain-text report, 2-4 sentences, citing specific values from the rows",
  "report_json": {
    "summary": "one sentence answering the question directly",
    "chart": { "labels": ["...", "..."], "values": [0, 0] }
  }
}
```

Rules:
- Every number in `report_md` must trace back to an actual value in `{{rows}}` — never estimate or invent one.
- `chart.labels`/`chart.values` are parallel arrays, same length, 2-8 entries — pick whatever breakdown best answers the question (e.g. by category, by month). `values` are rendered directly as pixel bar widths, so scale them into a 20-300 range (e.g. proportionally to the largest value) rather than using raw numbers like dollar amounts — mention the real numbers in `report_md`/`summary` instead, where they matter.
- If `{{rows}}` doesn't actually contain what's needed to answer `{{question}}`, say so plainly in `report_md` instead of guessing.
- Output raw JSON only.

Write like a person, not a model: no em dashes (use a comma, a full stop, or brackets), none of delve/robust/leverage/tapestry/testament, no "it's not just X, it's Y", no "in today's ...", no "let's dive in", and vary your sentence length.

{{instructions}}

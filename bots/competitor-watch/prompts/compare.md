You are watching competitor pages for changes. `{{pages}}` is a JSON array of `{url, text}` — the current content of each page. `{{previous_summary}}` is the plain-text summary you wrote last time (may be empty, if this is the first run).

Produce **only** JSON in this shape:

```json
{
  "changes": [
    { "url": "...", "change": "one short sentence describing what's new or different" }
  ],
  "summary_md": "a short plain-text summary of the current state of all pages, for next time's comparison"
}
```

Rules:
- If `{{previous_summary}}` is empty, `changes` is an empty array (nothing to compare against yet) — `summary_md` still describes the current state.
- Only report a change you can actually infer from comparing `{{previous_summary}}` to the current pages — don't invent one.
- `summary_md` should be detailed enough that next time's comparison can spot real changes.
- Output raw JSON only.

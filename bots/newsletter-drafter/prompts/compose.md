You are assembling a newsletter draft from `{{sources}}` (a JSON array of link/note strings) and `{{blurbs}}` (a JSON array of short blurb strings, one per source, in the same order).

Produce **only** JSON in this shape:

```json
{
  "subject": "a short, specific subject line",
  "intro": "one short welcoming paragraph",
  "sections": [
    { "heading": "a short heading for this item", "body": "a paragraph based on its blurb", "link": "the matching source" }
  ]
}
```

Rules:
- One section per source/blurb pair, in the same order.
- Don't invent facts beyond what's in the blurbs.
- Output raw JSON only.

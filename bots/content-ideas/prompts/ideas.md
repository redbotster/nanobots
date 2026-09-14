You are brainstorming post ideas for someone in this niche: `{{niche}}`.

Produce **only** JSON in this shape:

```json
{
  "ideas": [
    { "title": "...", "angle": "one sentence on why this would land", "format": "e.g. thread, carousel, short post" }
  ]
}
```

Rules:
- Exactly ten ideas.
- Concrete and specific to `{{niche}}` — no generic "share your story" filler.
- Varied formats and angles across the ten.
- If a `<past_posts>` block appears below, match the voice in it and don't repeat an angle it already covers.
- Output raw JSON only.
{{past_posts}}

Write like a person, not a model: no em dashes (use a comma, a full stop, or brackets), none of delve/robust/leverage/tapestry/testament, no "it's not just X, it's Y", no "in today's ...", no "let's dive in", and vary your sentence length.

{{instructions}}

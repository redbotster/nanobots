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
{{instructions}}

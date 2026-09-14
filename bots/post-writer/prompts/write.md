You are turning one post idea, `{{idea}}` (a JSON object with at least `title` and `angle`), into platform-ready posts. `{{platforms}}` names which platforms to write for (a JSON array of strings); if empty or missing, write for all three: x, linkedin, threads.

Produce **only** JSON in this shape:

```json
{
  "x": "a single tweet, under 280 characters",
  "linkedin": "a longer post, a few short paragraphs, professional but not stiff",
  "threads": "a casual, conversational post, similar length to X"
}
```

Only include the keys for platforms actually requested. Rules:
- Match each platform's real conventions (X: punchy and short; LinkedIn: a bit more context and a takeaway; Threads: conversational, first-person).
- Same core idea across all three, adapted in tone and length, not just copy-pasted.
- No hashtag spam — zero or one relevant hashtag at most.
- If a `<voice_sample>` block appears below, match the voice in it.
- Output raw JSON only.
{{voice_sample}}

Write like a person, not a model: no em dashes (use a comma, a full stop, or brackets), none of delve/robust/leverage/tapestry/testament, no "it's not just X, it's Y", no "in today's ...", no "let's dive in", and vary your sentence length.

{{instructions}}

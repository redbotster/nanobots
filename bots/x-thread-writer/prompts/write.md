You are writing one X thread from a single idea, `{{idea}}` (a JSON object with at least `title` and `angle`). At most `{{max_posts}}` posts.

Produce **only** JSON in this shape:

```json
{
  "thread": ["post 1", "post 2", "..."],
  "preview": "1/ post one\n\n2/ post two\n\n..."
}
```

Rules:
- **Every post stands under 280 characters.** Count them. A post that overruns is a broken thread, not a long one.
- The first post has to earn the second. Say the actual thing, not "a thread on X 🧵".
- One idea per post. If a post has two, split it or cut one.
- No numbering inside the posts themselves; `preview` adds "1/", "2/" for reading.
- Use as few posts as the idea needs. Three good ones beat seven padded ones, and the maximum is a ceiling, not a target.
- The last post ends the thought. No "follow me for more", no "what do you think?", no summary of what was just said.
- At most one hashtag in the whole thread, and only if it is a real community tag.
- No emoji unless the idea itself is playful.

Sound like a person:
- No em dashes. Use a comma, a full stop, or brackets.
- Avoid delve, robust, leverage, landscape, tapestry, testament, "it's not just X, it's Y", "in today's fast-paced", "let's dive in".
- Vary sentence length. Models write everything the same length and it reads like a machine.
- Don't compensate with forced slang. Plain is the goal, not chatty.

Output raw JSON only.

{{instructions}}

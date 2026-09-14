# tone

Rewrite text so it reads like a person wrote it, not a model.

`llm` harness, one `ai.generate` call. Editing only: every fact, name, number and link in the input survives. It does not write, summarise or improve the argument. Text that is mostly padding will come back much shorter, which is the job working.

Takes a `string` and returns a `string`, so it drops into the middle of anything that produces prose:

```yaml
- from: repurposer.linkedin_post
  to: cleanup.text
- from: cleanup.text
  to: notifier.message
```

Fan it out with `.*` to clean a list, and `join: lines` to collapse the results back (`docs/fan-out.md`).

`changed` reports what it fixed, one line per kind: "removed 3 em dashes", "cut the closing summary paragraph". An edit you cannot see is an edit you have to re-read the whole draft to trust. When the text is already fine it comes back byte for byte with `changed: []`.

`inputs.instructions` is the knob. The default is "Sound human. Don't sound like AI. No em dashes. Avoid the recognisable AI patterns." The specific list of tells lives in `prompts/rewrite.md`, because which patterns give a model away is the bot's job to know, not yours to spell out.

# Per-item fan-out

"For each overdue invoice, send a reminder" is what six catalog swarms describe and none of them did. They write `chaser.draft_ids.0` — the literal first element — and carry a comment apologising for it:

```yaml
# ... No per-item fan-out, so only the first overdue invoice's reminder
# gets sent per run.
```

A snap can now say **every** element instead of one:

```yaml
snaps:
  - from: chaser.draft_ids.*
    to: sender.draft_id
  - from: chaser.overdue.*.customer_email
    to: sender.summary
```

`sender` runs once per element. `*` was chosen over `[]` because the path is already dot-separated and already understands numeric indices, so `*` reads as "every index" in exactly the same position — and it survives YAML unquoted.

## What it does

- **The marker peels one list level**, exactly as an index does. `list<json>` + `.*` is `json`; `list<json>` + `.*.customer_email` is whatever that field is. It can only appear where there's a list to iterate, and only on the `from` side — the marker says *which list to walk*, not where to put each item.
- **Every marker snap into one bot iterates in lockstep** on a shared index. `chaser.overdue.*` and `chaser.draft_ids.*` into the same bot means element *i* of each, together. Two lists of different lengths would be a cross product, which is never what "for each" means, so the runner refuses.
- **A fanned-out bot's outputs become lists.** Running a bot twenty times produces twenty of each output, and `resolveEndpointType` wraps the type accordingly — so a downstream bot expecting a single value gets a type error at plan time rather than a surprise at run time. Read one back with `sender.message_id.0`, or fan the next bot out too.
- **Each item gets its own container and its own workspace** (`<run>/<bot>/item-N`), so one item's outputs can't be mistaken for the next one's.
- **An empty list is a successful no-op**, not a failure — a week with no overdue invoices shouldn't be a red run. Downstream sees an empty list rather than a missing output.

## Approvals: one gate for the batch

A fanned-out bot with an `approve` step opens **one** approval covering every item, and says so:

```
Send a reminder to client@example.com? — and 19 more like it
(20 in total, approving covers all of them)
```

This was a deliberate choice over one-approval-per-item. Twenty prompts means nobody reads the twentieth; it gets approved reflexively, which is worse than one prompt read properly. The trade is real and stated plainly: **it is a single click authorising twenty real sends**, so `BatchApprover` forces the count into the summary rather than letting a bot's single-item wording stand.

`BatchApprover` wraps `RunQueueApprover` rather than replacing it, so a batch approval is the same pending approval, in the same queue, answered the same way.

## Verified

Against real data, not fixtures. `content-ideas` generated 10 ideas from a live Shroud call; `post-writer` fanned out over them and produced 10 distinct posts, each tracking its own idea, in 10 separate containers. Before this, that swarm wrote one post and discarded nine ideas — which its own header comment admitted.

## Not done

The catalog's six `.0` swarms have **not** been converted. Each needs a decision the syntax can't make: `get-paid`'s final `notifier` would fan out to 20 Slack messages unless something joins the results first, and there is no join operation. Converting them is a per-swarm design question, not a mechanical edit.

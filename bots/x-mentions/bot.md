# x-mentions

Fetch recent posts that mentioned you on X.

`bare` harness, one `service.call`. Reads only: `writes_allowed` is empty, and there is no path in this bot that posts anything.

Every content swarm in this catalog was write-only before this. `repurpose-everything` publishes and then goes silent; nothing ever read what came back. This is the other half, and it snaps straight into `comment-responder`:

```yaml
- from: listener.mentions
  to: responder.comments
```

There is deliberately no `count` port: `join: count` on a snap already collapses a list to how many, so a second way to say it would be a second thing that can disagree (`docs/fan-out.md`).

`mentions` is `list<json>` with `id`, `text`, `author` (the @handle, stitched on from X's `author_id` expansion, because a numeric id is not something a human or a prompt can use) and `created_at`.

## Reads cost money

X removed its free tier in February 2026 and bills per post read, cheapest when an account reads its own mentions. `max_results` is therefore a budget, not a page size, and `since_id` is how you stop paying for the same posts twice: carry the newest id from the last run into the next one. Ceiling is 100, which is the endpoint's own.

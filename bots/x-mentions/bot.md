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

## Reads cost money, so it remembers where it got to

X removed its free tier in February 2026 and bills per post read, cheapest when an account reads its own mentions. `max_results` is therefore a budget, not a page size. Ceiling is 100, which is the endpoint's own.

This bot used to declare a `since_id` input whose description told you to "carry the last run's newest id here to stop paying for the same posts twice" — and nothing carried it. `listen-and-reply` passes only `max_results`, so every weekday run re-read the same twenty-five posts, paid for them again, and handed `comment-responder` mentions it had already drafted replies to. A port that tells you to do something the system gives you no way to do is worse than no port.

It now keeps the bookmark itself:

1. `memory.get newest_mention_id` — empty on the first ever run, which X reads as "no lower bound", so a new install sees the recent window once and pays for it once.
2. `mentions.list` with that as `since_id`.
3. `stop.if count == 0` — nothing new is the ordinary weekday outcome, and it ends the run quietly instead of running the drafting behind it (`docs/bot-contract.md`, `docs/runs.md`).
4. `memory.put` the bookmark, **after** the gate, so a run that stopped never moves it past posts nobody read.

The bookmark is X's own `meta.newest_id`, not an id this repo picks by sorting: snowflake ids are strings, and sorting them as strings breaks the day their length changes — quietly, months later, by re-reading and re-billing the whole window.

On `connection: demo` the fixture is a fixed world and always returns the same three mentions, so the demo path never takes the `stop.if`. That is the fixture being a fixture, not the bot failing to dedupe.

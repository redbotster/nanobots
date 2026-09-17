# follow-up-chaser

Find threads I sent that got no reply in N days and draft a polite nudge.

`openclaw` harness: fetches sent mail older than `inputs.days_silent` days, one `ai.generate` call decides which threads plausibly still need a nudge and writes one, then drafts (never sends) each via Gmail. Drafts only — sending is a separate brick (`email-send-approved`).

## It nudges a thread once, not every weekday

`never-drop-a-thread` runs this at 16:00 every weekday and sends what it drafts. The search was `in:sent older_than:3d`, and a thread that never gets a reply satisfies that every single day — so the same person got a nudge on Monday, another on Tuesday, another on Wednesday, indefinitely. One nudge is polite. Twenty is harassment with a cron expression behind it.

The search now excludes a label the bot puts on whatever it nudged:

```
in:sent older_than:3d -label:nanobots-nudged
```

Gmail answers "which of these have I not nudged yet" out of its own index. Nothing here keeps a set of thread ids, there is no state file to lose, and what the bot has done is visible in your own mailbox — you can look at the label, and you can remove it to nudge a thread again.

Two orderings that matter:

- The label goes on **after** the drafts exist. A thread marked nudged whose draft never got created is dropped silently and for good.
- Nothing stale is the ordinary outcome once the backlog clears, and the bot ends the run there rather than handing an empty list to the sender (`docs/runs.md`).

`nudged_label` is an input, so a swarm can use its own, and changing it starts the set again from scratch.

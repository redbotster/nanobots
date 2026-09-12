# post-publisher

Publish approved posts to X and LinkedIn.

`bare` harness: one unconditional approval gate, then publishes to both platforms.

X and LinkedIn both ship on `connection: demo` — no live posting client is built yet (see `docs/connections.md`). The catalog's own OAuth registry check says X/LinkedIn write scopes are likely already supported by 1Claw's `oauth_1claw` registry, but 1Claw's execution-intent binding *provisioning* isn't wired up in this build yet (see `internal/step/live.go`'s TODO) — the same gap blocking every non-Google/Slack/GitHub/Stripe/HubSpot provider.

`inputs.schedule` was declared and never read — publishing at a future time needs something to hold the post until then, and no such scheduler exists (the cron trigger schedules whole swarms, not individual posts). Removed rather than left as a port that quietly does nothing.

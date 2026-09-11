# email-send-approved

Send an existing draft after a human taps approve.

`bare` harness, three steps: gate, send, stamp. The gate is unconditional — this bot has no path from `draft_id` to `message_id` that skips it, and no swarm-level setting can remove it (see the comment in `nanobot.yaml`). If you need a bot that sends without asking, that's a different, deliberately-not-built bot — see `NANOBOTS-CATALOG.md`'s design rule: "Every brick that sends, posts, pays, or deletes has an approval gate by default. Users turn it off; we never ship it off."

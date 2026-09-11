# review-responder

Draft replies to new customer reviews in the business's tone.

`openclaw` harness: fetches new reviews, one `ai.generate` call drafts a reply to each. Drafts only — nothing is posted; the catalog calls for approval before posting, which is this brick's caller's job (a swarm snapping in `approve` before wherever the reply actually gets published).

Ships on `connection: demo` — Google Business Profile isn't in 1Claw's OAuth registry (confirmed in `NANOBOTS-CATALOG.md`'s own 1Claw-needs table) and needs its own dedicated OAuth app, the same situation Gmail was in before `internal/google` existed. Out of scope for this pass; see `docs/connections.md`.

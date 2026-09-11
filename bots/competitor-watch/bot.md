# competitor-watch

Check a list of pages weekly and report what changed.

`bare` harness (deliberately, with `ai.generate` — see `docs/harnesses.md`): fetches each URL in `inputs.urls` via `web.fetch`, compares against the last run's summary (stored in 1Claw agent memory, best-effort), and reports what's new.

Memory here only remembers the last written *summary*, not full page snapshots — a lightweight, "good enough for a weekly digest" design, not a real diffing engine.

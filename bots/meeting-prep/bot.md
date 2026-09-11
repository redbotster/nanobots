# meeting-prep

For each meeting today, pull related mail and write a one-page brief.

`openclaw` harness: fetches today's calendar events and recent mail, then one `ai.generate` call turns them into a `briefs` list (one per meeting) and a rendered PDF (`brief_md`, despite the name — rendered like every other document in this catalog, via `transform.render` to PDF, not a raw `.md` file).

Read-only everywhere — this bot never modifies a calendar event or a message.

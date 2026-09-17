# calendar-scheduler

Read a scheduling request and draft a reply with three open slots.

`openclaw` harness: pulls the connected calendar's busy times, then one `ai.generate` call proposes three open slots (respecting `inputs.working_hours`) and writes a reply. The reply is saved as a Gmail draft — never sent, and never creates a calendar event on its own, matching the catalog's "never creates events without approval" rule for this brick.

`to` and `subject` are inputs, not fields read out of `thread`. They used to be `{{inputs.thread.from}}` and `"Re: {{inputs.thread.subject}}"`, which is right for a mail thread and resolved to nothing when `lead-to-meeting` snapped a lead record in — the bot drafted an email to `""` with the subject `"Re: "`, and every layer above reported success. `thread` is now context for the model to write a reply about; the address is a declared field, so no line of prose inside the data can move it.

# calendar-scheduler

Read a scheduling request and draft a reply with three open slots.

`openclaw` harness: pulls the connected calendar's busy times, then one `ai.generate` call proposes three open slots (respecting `inputs.working_hours`) and writes a reply. The reply is saved as a Gmail draft — never sent, and never creates a calendar event on its own, matching the catalog's "never creates events without approval" rule for this brick.

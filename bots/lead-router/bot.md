# lead-router

Create or update the CRM contact and alert the right person.

`bare` harness, deterministic: upserts the lead into HubSpot, then posts a Slack alert. `owner_rules` (free text describing who should be alerted) is passed through into the alert message for a human to read, not parsed into actual routing logic — a real "route to the right person" implementation needs a small rules engine this build doesn't have; the message just states the rule so a human/Slack workflow can act on it.

Both services ship on `connection: demo`. Slack here goes through `service.call` (a `provider: slack` service), not the `notify` step's `"slack:..."` channel convention `bots/notify` uses — there's no live dispatch wired for `provider: slack` via `service.call` yet (only via `notify`), so this brick's live behavior falls through to the generic 1Claw execution-intent path if ever switched off `demo`, the same pre-existing gap every non-Google/GitHub/Stripe/HubSpot/Slack-via-notify provider has.

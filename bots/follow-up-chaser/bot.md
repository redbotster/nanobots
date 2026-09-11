# follow-up-chaser

Find threads I sent that got no reply in N days and draft a polite nudge.

`openclaw` harness: fetches sent mail older than `inputs.days_silent` days, one `ai.generate` call decides which threads plausibly still need a nudge and writes one, then drafts (never sends) each via Gmail. Drafts only — sending is a separate brick (`email-send-approved`).

# support-triage

Categorise new support mail, draft a reply, and flag anything needing a human.

`openclaw` harness: fetches support mail, one `ai.generate` call categorises each into a ticket, drafts a reply, and flags anything with refund/legal keywords (or anything else genuinely ambiguous) as an escalation instead of drafting a reply for it. Drafts only — nothing is ever sent.

`kb` (an optional knowledge-base file input) is accepted but not read into the prompt yet — the same disclosed gap other bots' unused optional file inputs have.

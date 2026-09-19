# lead-enricher

Given a name, email, or company, find public context and score fit against my ICP.

`agent.loop` (v0.2.0, `docs/agent-loop.md`): the model decides whether the lead has enough to search HubSpot for an existing contact (an email, not just a name or company — a search with nothing to search by can't find anything) before scoring fit against `inputs.icp` and producing an enriched profile. Public sources only, no data brokers, no personal-life lookups — enforced by what the goal instructs, since there's no actual data-broker integration built to misuse in the first place.

The catalog also lists "web fetch" for this brick (to pull in public context); no live web lookup is wired up here yet — the same disclosed gap `content-ideas` has for the same reason (no real search capability, just arbitrary URL fetching, which isn't useful without knowing which URL to fetch for an arbitrary lead).

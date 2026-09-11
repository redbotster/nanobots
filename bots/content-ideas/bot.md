# content-ideas

Suggest ten post ideas for my niche based on what I've written.

`openclaw` harness, one `ai.generate` call brainstorming from `inputs.niche` alone.

Two catalog-declared capabilities aren't wired up yet, honestly, not silently: `past_posts` (an optional file input) is accepted but never read into the prompt — the same disclosed gap `draft-replies`' `voice_sample` input has. And the catalog lists "web fetch" as a service for pulling in what's actually trending; no live trend source is fetched here (a real "what's trending" signal needs a search/trends API, not just fetching arbitrary URLs, which is out of scope) — ideas come from the model's own knowledge of the niche, not live data. No publishing capability at all; this bot only ever produces ideas.

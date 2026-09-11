# Connections

A design principle carried through the whole build: wherever a human would normally need to understand OAuth, an API key, a redirect URI, or a binding id, Nanobots instead shows one button and picks the mechanism itself.

`schema.ConnectionMethod` (`internal/schema/types.go`) is that abstraction — a layer Nanobots puts on top of whatever 1Claw exposes natively, set per-service on a `nanobot.yaml`'s `services[].connection`:

| Value | Mechanism | When |
|---|---|---|
| `oauth_1claw` | 1Claw's own OAuth provider registry, `POST /v1/agents/{id}/oauth/connect` | A provider 1Claw already supports (its registry lists `openid, email, profile, calendar, drive` today — no Gmail scopes) |
| `oauth_native` | A provider-specific OAuth client Nanobots implements directly | A provider 1Claw doesn't support natively but has a normal OAuth flow (e.g. a dedicated Google client) |
| `browser` | [1Claw Browser Bridge](https://docs.1claw.co/docs/agents/browser-bridge) — pair a device once, then the bot drives a real, already-authenticated browser | Services with no OAuth at all, or a login flow that's just easier to click through than to integrate |
| `api_key_vault` | A human pastes a key once; it goes straight into a 1Claw vault secret | Last resort — still never touches the bot or nanobotd's disk |
| `demo` | Fixture data, no network call | Explicitly marked on a service when real access isn't wired up yet |

## Why Gmail/Drive are `demo` right now

Both example bots' Google services are `connection: demo`. The obvious next step — Browser Bridge driving a real logged-in browser to Gmail — was tried and hit a wall: **Google refuses sign-in outright on any CDP/automation-controlled Chrome instance**, confirmed with screenshots (`"This browser or app may not be secure"`), not a guess. That's a deliberate Google policy, not a bug in the bridge or in this code, and not something to work around — spoofing the automation flag to get past Google's own bot detection is out of bounds.

Browser Bridge itself works exactly as documented (see `docs/browser-bridge.md`) and remains the intended `connection` for services that don't do this — Stripe, HubSpot, X/LinkedIn dashboards, and the rest of the catalog's later tranches.

Real Gmail/Drive access is deferred pending one of:
- adding `gmail.*` scopes to 1Claw's own Google OAuth provider (`oauth_1claw` becomes viable), or
- a dedicated Google OAuth client Nanobots ships and manages itself (`oauth_native`).

## Try it

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

Everything in this swarm except the two Google services runs for real — real 1Claw agents, real Shroud, a real run-level approval. The Google calls serve fixture data from each bot's `fixtures/*.json` instead.

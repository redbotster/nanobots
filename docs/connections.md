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

## Why every bot's Google service is `demo` right now

Every bot's Google service (Gmail, Drive, Sheets) currently ships as `connection: demo`, even though real Gmail/Drive/Sheets access is now implemented (`internal/google` — PKCE OAuth, no client secret, direct against Google, no browser automation). The obvious first approach — Browser Bridge driving a real logged-in browser to Gmail — was tried and hit a wall: **Google refuses sign-in outright on any CDP/automation-controlled Chrome instance**, confirmed with screenshots (`"This browser or app may not be secure"`), not a guess. That's a deliberate Google policy, not a bug in the bridge or in this code, and not something to work around — spoofing the automation flag to get past Google's own bot detection is out of bounds.

Browser Bridge itself works exactly as documented (see `docs/browser-bridge.md`) and remains the intended `connection` for services that don't do this — Stripe, HubSpot, X/LinkedIn dashboards, and the rest of the catalog's later tranches.

### Turning on real Gmail/Drive/Sheets (`oauth_native`)

1. Create a Google Cloud OAuth client: **APIs & Services → Credentials → Create Credentials → OAuth client ID → Desktop app**. Testing mode is enough — no Google verification review needed for personal use. Enable the Gmail API and Drive API (and Sheets API if you use `form-to-sheet`) for the project.
2. Add `GOOGLE_OAUTH_CLIENT_ID=...` to `~/.secrets/nanobots.env`, alongside `ONECLAW_API_KEY`.
3. Run `nanobots connect google` once — it opens your browser to Google's real consent screen, then stores the resulting refresh token in a 1Claw vault secret (`nanobots-main` vault, `google/refresh_token`), never on local disk.
4. Change a bot's `services[].connection` from `demo` to `oauth_native` in its `nanobot.yaml`.
5. Restart `nanobotd` (or `nanobots up`) so it picks up the vault.

From then on, every Google-provider service across every bot shares that one connected account (`internal/step/google_live.go`'s `GoogleConfig` is process-wide, not per-bot) — see that file for exactly which `op:` values (`messages.list`, `files.create`, `rows.append`, ...) are implemented and any known shape gaps (e.g. `files.create` has no `filename` input yet, so one gets synthesized; `rows.append` has no column-order mapping yet, so values go in alphabetical-by-key order).

## Try it

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

With every Google service still on `connection: demo`, everything in this swarm except the two Google calls runs for real — real 1Claw agents, real Shroud, a real run-level approval. The Google calls serve fixture data from each bot's `fixtures/*.json` instead.

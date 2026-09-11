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
3. Restart `nanobotd`, then click **Connect** next to Google on the WebUI's **Settings** page (or run `nanobots connect google` from the CLI) — it opens your browser to Google's real consent screen, then stores the resulting refresh token in a 1Claw vault secret (`nanobots-main` vault, `google/refresh_token`), never on local disk.
4. Change a bot's `services[].connection` from `demo` to `oauth_native` in its `nanobot.yaml`.

From then on, every Google-provider service across every bot shares that one connected account (`internal/step/google_live.go`'s `GoogleConfig` is process-wide, not per-bot) — see that file for exactly which `op:` values (`messages.list`, `files.create`, `rows.append`, ...) are implemented and any known shape gaps (e.g. `files.create` has no `filename` input yet, so one gets synthesized; `rows.append` has no column-order mapping yet, so values go in alphabetical-by-key order).

## Slack and GitHub (`api_key_vault`)

Slack and GitHub both use plain, non-expiring tokens rather than an OAuth flow, so connecting either is just: **Settings → paste the token → Save** (or `nanobots connect slack` / `nanobots connect github` from the CLI). Same storage as Google — a 1Claw vault secret, never local disk, never inside a bot container.

- **Slack**: create a bot token at [api.slack.com/apps](https://api.slack.com/apps), scoped to `chat:write`, and invite the bot to whatever channel it should post in. `bots/notify`'s `channel` input recognizes a `"slack:#channel-name"` or `"slack:C0123..."` value and posts there for real once connected (`internal/step/slack_live.go`) — any other channel prefix (`"email:..."`, `"sms:..."`) still has no live backend and stays a documented no-op.
- **GitHub**: create a personal access token scoped to `repo` (or `public_repo` for public repos only). `bots/github-issues-digest` uses it via a normal `service.call` with `provider: github`, same pattern as Google.

Unlike Google, there's no `connection: demo` to flip on `notify` itself (it isn't tied to a `services:` entry) — a `"slack:..."` channel always tries the real backend once one is configured, and errors clearly if it isn't, rather than silently no-op'ing a message the caller thinks was sent.

### A real operational note: 1Claw vault passkey verification

Depending on your account's vault security tier, 1Claw may require a passkey unlock before `GetSecret` succeeds — you'll see a 403 `"Passkey verification required to access vault secrets"` from 1Claw itself surfaced through whichever bot tried to read a connected credential. That's a genuine security feature of your 1Claw account, not a Nanobots bug, and not something this codebase tries to route around: unlock your vault with your passkey (1Claw's own dashboard/CLI) and retry.

## Try it

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

With every Google service still on `connection: demo`, everything in this swarm except the two Google calls runs for real — real 1Claw agents, real Shroud, a real run-level approval. The Google calls serve fixture data from each bot's `fixtures/*.json` instead.

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

Browser Bridge itself works exactly as documented (see `docs/browser-bridge.md`) and remains the intended `connection` for services with no viable OAuth or static-token path — today that's just Google Business Profile replies (`review-responder`), which stays on `connection: demo` until a dedicated OAuth app exists for it (Business Profile is itself a Google API, so the more likely real path is actually extending `internal/google` with its scope, the same way Calendar was added, rather than Browser Bridge — just not done yet). Stripe, HubSpot, X, and LinkedIn all turned out not to need it: the first two have long-lived static tokens (below), and X/LinkedIn have normal OAuth2 flows of their own (next section).

### Turning on real Gmail/Drive/Sheets (`oauth_native`)

1. Create a Google Cloud OAuth client: **APIs & Services → Credentials → Create Credentials → OAuth client ID → Desktop app**. Testing mode is enough — no Google verification review needed for personal use. Enable the Gmail API and Drive API (and Sheets API if you use `form-to-sheet`) for the project.
2. Add `GOOGLE_OAUTH_CLIENT_ID=...` to `~/.secrets/nanobots.env`, alongside `ONECLAW_API_KEY`.
3. Restart `nanobotd`, then click **Connect** next to Google on the WebUI's **Settings** page (or run `nanobots connect google` from the CLI) — it opens your browser to Google's real consent screen, then stores the resulting refresh token in a 1Claw vault secret (`nanobots-main` vault, `google/refresh_token`), never on local disk.
4. Change a bot's `services[].connection` from `demo` to `oauth_native` in its `nanobot.yaml`.

From then on, every Google-provider service across every bot shares that one connected account (`internal/step/google_live.go`'s `GoogleConfig` is process-wide, not per-bot) — see that file for exactly which `op:` values (`messages.list`, `files.create`, `rows.append`, `events.list`, ...) are implemented and any known shape gaps (e.g. `files.create` has no `filename` input yet, so one gets synthesized; `rows.append` has no column-order mapping yet, so values go in alphabetical-by-key order). `ScopeCalendarReadonly` is included in `DefaultScopes`, so `meeting-prep`/`calendar-scheduler`'s calendar reads work the same way once connected — no separate connect step.

## Turning on real X/LinkedIn posting (`oauth_native`)

`post-publisher`'s two services (`x`, `linkedin`) ship as `connection: demo` by default, same as Google — real posting is implemented (`internal/x`, `internal/linkedin`) but off until you connect an account. Both build on a small, shared, provider-agnostic OAuth2+PKCE core (`internal/oauth2pkce`) rather than each hand-rolling the loopback-redirect dance Google's older client wrote from scratch.

**X (Twitter)**: create an OAuth 2.0 app at [developer.x.com](https://developer.x.com/en/portal/dashboard) with **App type: Native App** (a public client — no secret, PKCE only, the same installed-app pattern as Google's Desktop client) and request the `tweet.read`, `tweet.write`, `users.read`, and `offline.access` scopes (that last one is what makes X return a refresh token at all). Add `X_OAUTH_CLIENT_ID=...` to `~/.secrets/nanobots.env`, restart `nanobotd`, then click **Connect** next to X in Settings.

**LinkedIn**: create an app at [developer.linkedin.com](https://www.linkedin.com/developers/apps) with the **Share on LinkedIn** and **Sign In with LinkedIn using OpenID Connect** products added, which grants the `w_member_social`, `openid`, and `profile` scopes. Unlike X, LinkedIn's OAuth2 implementation isn't a true public client — it requires a client secret on the token exchange even with PKCE also in play — so add both `LINKEDIN_OAUTH_CLIENT_ID=...` and `LINKEDIN_OAUTH_CLIENT_SECRET=...`. Also unlike X, a default LinkedIn app doesn't get a refresh token at all (that needs LinkedIn's separate "Programmatic Refresh Tokens" product access, not assumed here) — if none comes back, the access token itself is stored and used as-is, good for about 60 days, after which reconnecting from Settings is the only option (`internal/step/linkedin_live.go`'s `linkedinTokenSource` documents this fallback in code).

Change `post-publisher`'s `services[].connection` from `demo` to `oauth_native` for `x` and/or `linkedin` independently — you don't need both connected to use one.

## Slack, GitHub, Stripe, and HubSpot (`api_key_vault`)

All four use plain, non-expiring tokens rather than an OAuth flow, so connecting any of them is just: **Settings → paste the token → Save** (`POST /api/connections/{service}/token`, `internal/api/connections.go`). Same storage as Google — a 1Claw vault secret, never local disk, never inside a bot container. Unlike Google, there's no CLI equivalent yet (`nanobots connect` only implements `google`, below) — the WebUI is the only way to connect these four today.

- **Slack**: create a bot token at [api.slack.com/apps](https://api.slack.com/apps), scoped to `chat:write`, and invite the bot to whatever channel it should post in. `bots/notify`'s `channel` input recognizes a `"slack:#channel-name"` or `"slack:C0123..."` value and posts there for real once connected (`internal/step/slack_live.go`) — any other channel prefix (`"email:..."`, `"sms:..."`) still has no live backend and stays a documented no-op.
- **GitHub**: create a personal access token scoped to `repo` (or `public_repo` for public repos only). `bots/github-issues-digest` uses it via a normal `service.call` with `provider: github`, same pattern as Google.
- **Stripe**: create a secret key (test or live) at [dashboard.stripe.com/apikeys](https://dashboard.stripe.com/apikeys). `bots/invoice-chaser` uses it via `provider: stripe`, `internal/step/stripe_live.go` (`InvoicesList`).
- **HubSpot**: create a private app token under **Settings → Integrations → Private Apps**, scoped to `crm.objects.contacts.read`/`.write`. `bots/lead-enricher` and `bots/lead-router` use it via `provider: hubspot`, `internal/step/hubspot_live.go` (`ContactSearch`/`ContactUpsert`). The catalog describes this bot pair as "HubSpot or Salesforce" — only HubSpot is wired, on the theory that one real integration beats two half-built ones.

Unlike Google, there's no `connection: demo` to flip on `notify` itself (it isn't tied to a `services:` entry) — a `"slack:..."` channel always tries the real backend once one is configured, and errors clearly if it isn't, rather than silently no-op'ing a message the caller thinks was sent.

## `web.fetch` — no connection at all

`content-ideas` and `competitor-watch` use a `web.fetch` step (a plain outbound GET plus a tag-stripping regex to pull out text, capped at 1MB — "good enough for a summary prompt, not a real readability extractor"). It has no `connection:` concept, no vault, no OAuth, and nothing to set up: `DemoDeps` serves a fixture, `LiveDeps` hits the real URL, `RemoteDeps` proxies the container's request through nanobotd like every other live step (`internal/step/webfetch.go`).

### A real operational note: 1Claw vault passkey verification

Depending on your account's vault security tier, 1Claw may require a passkey unlock before `GetSecret` succeeds — you'll see a 403 `"Passkey verification required to access vault secrets"` from 1Claw itself surfaced through whichever bot tried to read a connected credential. That's a genuine security feature of your 1Claw account, not a Nanobots bug, and not something this codebase tries to route around: unlock your vault with your passkey (1Claw's own dashboard/CLI) and retry.

## Try it

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

With every Google service still on `connection: demo`, everything in this swarm except the two Google calls runs for real — real 1Claw agents, real Shroud, a real run-level approval. The Google calls serve fixture data from each bot's `fixtures/*.json` instead.

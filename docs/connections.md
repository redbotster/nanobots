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

All four use plain, non-expiring tokens rather than an OAuth flow, so connecting any of them is just: **Settings → paste the token → Save** (`POST /api/connections/{service}` with `{"token": "..."}`, `internal/api/connections.go`). Same storage as Google — a 1Claw vault secret, never local disk, never inside a bot container. Unlike Google, there's no CLI equivalent yet (`nanobots connect` only implements `google`, below) — the WebUI is the only way to connect these four today.

- **Slack**: create a bot token at [api.slack.com/apps](https://api.slack.com/apps), scoped to `chat:write`, and invite the bot to whatever channel it should post in. `bots/notify`'s `channel` input recognizes a `"slack:#channel-name"` or `"slack:C0123..."` value and posts there for real once connected (`internal/step/slack_live.go`) — any other channel prefix (`"email:..."`, `"sms:..."`) still has no live backend and stays a documented no-op.
- **GitHub**: create a personal access token scoped to `repo` (or `public_repo` for public repos only). `bots/github-issues-digest` uses it via a normal `service.call` with `provider: github`, same pattern as Google.
- **Stripe**: create a secret key (test or live) at [dashboard.stripe.com/apikeys](https://dashboard.stripe.com/apikeys). `bots/invoice-chaser` uses it via `provider: stripe`, `internal/step/stripe_live.go` (`InvoicesList`).
- **HubSpot**: create a private app token under **Settings → Integrations → Private Apps**, scoped to `crm.objects.contacts.read`/`.write`. `bots/lead-enricher` and `bots/lead-router` use it via `provider: hubspot`, `internal/step/hubspot_live.go` (`ContactSearch`/`ContactUpsert`). The catalog describes this bot pair as "HubSpot or Salesforce" — only HubSpot is wired, on the theory that one real integration beats two half-built ones.

Unlike Google, there's no `connection: demo` to flip on `notify` itself (it isn't tied to a `services:` entry) — a `"slack:..."` channel always tries the real backend once one is configured, and errors clearly if it isn't, rather than silently no-op'ing a message the caller thinks was sent.

## `web.fetch` — no connection at all

`content-ideas` and `competitor-watch` use a `web.fetch` step (a plain outbound GET plus a tag-stripping regex to pull out text, capped at 1MB — "good enough for a summary prompt, not a real readability extractor"). It has no `connection:` concept, no vault, no OAuth, and nothing to set up: `DemoDeps` serves a fixture, `LiveDeps` hits the real URL, `RemoteDeps` proxies the container's request through nanobotd like every other live step (`internal/step/webfetch.go`).

### A real operational note: 1Claw vault passkey verification

Depending on your account's vault security tier, 1Claw may require a passkey unlock before `GetSecret` succeeds — you'll see a 403 `"Passkey verification required to access vault secrets"` from 1Claw itself, surfaced through whichever bot tried to read a connected credential. That's a genuine security feature of your 1Claw account, not a Nanobots bug, and not something this codebase tries to route around.

**v3 Phase 4 changed what happens next.** A run that hits this no longer fails outright — it pauses with status `awaiting_unlock` (the Runs page and the swarm card both say so, in words distinct from an approval: "waiting on 1Claw's vault", not "waiting for your approval," since there's no button in this app that answers it) and retries automatically every 30 seconds until either the vault unlocks or you stop the run. Unlock it with your passkey in 1Claw's own dashboard or CLI; nothing here needs a manual re-run. This doesn't count against the bot's own `retry:` budget — a locked vault could clear in a minute or sit for the rest of the day, and neither is the bot's own transient failure. `GET /api/status`'s `vault_locked`/`vault_reason` report the same condition proactively, before you've even pressed Run.

## Try it

```
nanobots run -f examples/swarms/daily-email-recap.yaml
```

With every Google service still on `connection: demo`, everything in this swarm except the two Google calls runs for real — real 1Claw agents, real Shroud, a real run-level approval. The Google calls serve fixture data from each bot's `fixtures/*.json` instead.


## Connecting an account, then actually using it

These are two separate things, and the second one used to have no bulk path.

Every bot ships on `connection: demo`. That is the right default — nothing should read a real inbox or write to a real Drive until a human says so. But it means connecting an account changes nothing on its own, and the catalog is lopsided: **24 of the 34 declared services are Google**. Measured on a fresh install, every swarm sits entirely on demo data. Getting one of them live meant one OAuth round trip followed by up to twenty-four individual toggles, hunted down one bot at a time in the bot library.

Settings now says, per provider, what connecting it would get you, and offers the second half in one action:

```
Google    Gmail, Drive, Sheets, Calendar — opens your browser to sign in   [Connect]
          18 bots would use this once connected
```

and once connected:

```
          18 bots still on demo data   [Use my account in all 18]
          6 bots using your account    [Back to demo]
```

- `GET /api/connections/{service}/bots` returns the bot ids split by demo/live, so the count is real rather than a guess.
- `POST /api/connections/{service}/bots` with `{"live": true|false}` switches them all.

**Going live is gated** exactly as the single-bot toggle is: the credential must genuinely be in the vault first, or the request is refused with `google isn't connected yet — connect it from Settings first`. This is the action that makes bots touch real accounts, so it cannot happen by accident.

**Going back to demo is never gated.** Undoing should always be at least as easy as doing.

Each bot's `nanobot.yaml` is edited with the same surgical line editor the single-bot toggle uses, so every comment in the file survives, and the result is re-parsed before it is written — a text edit that produced something unloadable never reaches disk. A bot that fails to switch is reported by name rather than silently skipped or rolled back: the bots that did switch really did switch, and claiming otherwise would be worse.

## Asking "what is connected" used to be the slowest thing in the app

`GET /api/connections` reads eight vault secrets — seven services plus
LinkedIn's fallback — and 1Claw is roughly 900ms away and throttles
concurrent reads. Sequentially that was 7.8s. Concurrently it is 3.5s, not
the ~1s the arithmetic suggests, because of the throttling.

Four screens fetch it on mount: Settings, the bot library, the builder, and
the getting-started card, which is on the landing page. So the app's very
first screen opened by waiting on it.

Three things fixed it, and they are independent — each covers a case the
others do not:

- **Single-flight.** A plain cache made the cold case *worse* than none: four
  concurrent requests took 15.7s each, against 7.8s for one uncached
  request, because each fans out to eight throttled reads and thirty-two at
  once queue behind each other. Callers arriving together now wait on one
  computation.
- **Invalidate on write, not a short TTL.** Every handler that writes a
  credential drops the cache, so connecting an account shows up immediately.
  The TTL is only a backstop for a secret changed in 1Claw's own dashboard
  or by another install.
- **Serve the stale answer while refreshing.** The backstop used to be
  something a *person* waited for: with a one-minute TTL, whichever page
  load first crossed the minute paid 3.5s again. An expired value is now
  returned immediately and corrected behind the request. A cold cache still
  blocks — there is nothing to be stale with, and answering "nothing is
  connected" because the answer has not arrived would be the app claiming
  something untrue about a real account.

Plus a warm-up at startup (`Server.Warm`), because the seconds between the
daemon binding its port and a human opening a browser are free.

Measured against the live account, end to end:

| | before | after |
|---|---|---|
| first page load after a restart | 3.5s | **0.99ms** |
| the first load after each TTL expiry | 3.5s | **1.2ms** |

One bug in that change only a stopwatch found, worth knowing if you write
another cache like this: the first version served stale only when no refresh
was in flight, so the *second* request during a refresh fell through and
waited on it — 0.97ms, then 3.29s, then 2ms. Whether to *start* a refresh
depends on whether one is running; whether to *wait* for it does not.

## Demo data is never silent

Every bot ships on `connection: demo`, so the default experience is a run
that succeeds, produces plausible emails and invoices, and is
indistinguishable from a real one. That is the most misleading thing this
product can do, and for a long time nothing said otherwise — not even the
log, which printed `gmail.messages.list -> ok` for fixture data exactly as
it does for a real call.

Now it says so in three places, each where someone actually is:

- **the log**, per call: `gmail.messages.list -> ok (demo data — not your real google)`
- **the run**, aggregated: "gmail, gdrive were answered from this repo's example data, not your account", with a button to connect one
- **Settings**, which already counts how many bots are still on demo data per provider and switches them over in one click

## Adding a provider

Everything a connected service needs now lives in one place per layer,
which it did not before: the same seven configs were resolved as a struct
in `internal/wiring`, unpacked into seven positional arguments to
`BuildDeps` (which had reached seventeen parameters — a signature where
transposing two strings still compiles), stored as seven fields on the
Orchestrator, and assigned one by one onto `LiveDeps`. Adding a provider
meant editing all four and hoping you caught every name.

Now:

1. Add a `<name>Config` field to `step.ServiceConfigs` (`internal/step/services.go`).
2. Fill it in `wiring.BuildServiceConfigs` when its credential is present.
3. Register a dispatcher in `serviceDispatchers`, keyed by the string a bot
   writes in `services[].provider`.

Nothing between those three points needs to change — the config travels as
one value from where it's resolved to where it's used, and
`LiveDeps.ServiceCall` looks the provider up rather than testing for it.

`step.LiveServiceProviders()` enumerates what's registered, so a provider
with no direct integration fails with a message naming what this build
*can* do, from the registry itself rather than from a list maintained
beside it. Before, that path fell through to a nil 1Claw client and
panicked.

## Reading from X and LinkedIn

The catalog could publish to both and read from neither, so every content
swarm was write-only: `repurpose-everything` posts and then goes silent.
`x-mentions` and `linkedin-comments` are the read side. They have very
different prospects, and it is worth being blunt about which.

### X mentions: real, and metered

`GET /2/users/{id}/mentions` works with the OAuth connection this repo
already sets up. X removed its free tier in February 2026 and moved to
pay-per-use, billing per post read and charging least when an account reads
its own mentions. So `max_results` on `x-mentions` is a budget rather than a
page size, and `since_id` is how you avoid paying twice for the same post:
carry the newest id from one run into the next.

### LinkedIn comments: real, behind an approval

Reading comments uses the **Community Management API**. That is a separate
product you add to your LinkedIn app, and LinkedIn reviews and approves it
per app; it is not part of the default "Sign In with LinkedIn" scopes an
OAuth connect gives you. Without it the API answers 403, and
`internal/linkedin` turns that into a sentence naming the product to apply
for rather than surfacing a bare status code.

### LinkedIn messages: no path, and not a todo

`linkedin-dm-triage` takes messages as `list<json>` input and does not
declare a `services:` entry, because there is nothing honest to declare.
LinkedIn's Messages API is write-only: it can send to a first-degree
connection or reply into a thread, and there is no endpoint for listing
conversations or reading history. Reading a member's mailbox exists only
through the Compliance Events API, restricted to FINRA/SEC-registered
archiving vendors. Products advertising "read your LinkedIn inbox" do it by
driving a real logged-in session, against LinkedIn's terms.

This is the one gap in this file that is not a todo. If LinkedIn ever opens
the endpoint, a fetcher bot snaps into `linkedin-dm-triage.messages` and
nothing about the bot changes.

## Slack was connectable and unreachable

Settings offered a Slack bot-token row, `internal/slack` had a working
client, and the `notify` step used it for channels written as
`slack:#name`. But `lead-router` does not notify: it calls `service.call`
with `service: slack, op: messages.post`, and `serviceDispatchers` had no
`slack` entry. So connecting Slack in the app changed nothing for that bot,
which still failed with "this build has no direct slack integration (it
has: [github google hubspot linkedin stripe x])" — while the client it
needed sat one function away.

The dispatcher is registered now, with one op, because one is what the
catalog uses. `TestEveryProviderTheCatalogDeclaresCanBeDispatched` reads
every `services:` block in `bots/` and fails if a declared provider has no
dispatcher, so the next one cannot be offered-but-unreachable in the same
way.

`google_business_profile` is the remaining exception and is listed as such
in that test: `review-responder` declares it and no client exists yet.

## The daemon no longer answers to every website

`nanobotd` binds loopback, and for a long time it also answered
`Access-Control-Allow-Origin: *` on every route, including POST.

Loopback is no defence against that. The browser is already inside the
loopback, so any page the user happened to have open could read every swarm
and run, read a webhook token, start a run, save a swarm, and answer a
pending approval. Answering an approval is how a swarm sends mail or pays an
invoice. The JSON content type makes those requests preflighted, and a
wildcard passes the preflight, so the protection people assume preflight
gives was not there.

Two places in the code already treated the wildcard as a hazard and worked
around it locally: `safeBlobMime` allowlists blob content types so a page
cannot get scripted content executing on the daemon's origin, and
`handleSwarmYAML` was fixed to resolve `?path=` inside the swarms directory
after `?path=/Users/you/.ssh/id_rsa` turned out to work. Both are still
right. They were compensating for the policy rather than fixing it.

The stated reason for the wildcard was that `vite dev` is a different
origin. It is not: vite proxies `/api` to the daemon server-side
(`web/vite.config.ts`), every path the frontend fetches is relative, and the
browser never makes a cross-origin request to nanobotd at all. The wildcard
bought the app nothing.

Now only loopback origins are echoed, so serving the built UI from any local
port still works. The origin is *parsed*, not prefix-matched, because
`http://127.0.0.1.evil.example` starts with something that looks right and a
browser will send it from an attacker's page. A request with no `Origin` at
all — curl, the vite proxy, a bot container — is not a browser cross-origin
request and gets no CORS headers, because it needs none. `Vary: Origin` is
set either way, since these routes carry ETags and a cache must not hand one
origin's response to another.

## web.fetch cannot reach your machine

`web.fetch` takes a URL from a bot's inputs, and the fetch runs **in
nanobotd**, not in the bot's container. That means it inherits the daemon's
network position, which includes loopback.

Demonstrated before this was fixed. A swarm using `competitor-watch` (the
one bot declaring `network_egress: ["*"]`) with

```yaml
urls: ["http://127.0.0.1:7474/api/swarms/lead-to-meeting/webhook"]
```

ran to success, and nanobotd's own webhook token came back in the run's
output. A swarm that then notifies or emails has exfiltrated it. The same
reach covered `169.254.169.254` on a cloud VM and every other service on the
host or LAN. The likely delivery is an imported swarm bundle: `nanobots
import` shows what a swarm will *write* to, and a fetch is a read.

`EgressPolicy` did not help and was never meant to. It answers *which public
hosts* a bot promised to visit. A bot declaring `"*"` is saying it reads
arbitrary pages from the web, not that it may read the machine it runs on,
and an empty list means unenforced, which most bots are.

Loopback, link-local (cloud metadata), private and multicast addresses are
now refused. The check runs in the dialer's `Control` hook, at connect time,
on the address actually being dialled:

- A hostname resolves to whatever its owner says. `localtest.me` already
  points at 127.0.0.1, so blocking on the string "127.0.0.1" catches
  nothing.
- A hostile name can answer differently on a second lookup, so a
  check-then-connect passes the check and connects somewhere else. `Control`
  runs after resolution with no gap.
- Redirects get the same treatment, because the hook runs per connection
  rather than per request.

Public fetches are unaffected: `competitor-watch` against `https://example.com/`
still succeeds, and the bots that exist to read the web keep working.

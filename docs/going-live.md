# Going live: from demo data to real accounts

Everything on this page is optional. With nothing configured at all,
`nanobots up` runs the whole catalog against fixtures — a fresh clone can
run a four-bot swarm to success before you have set up anything. This page
is about making it real.

There are three layers, and each is useful on its own.

## 1. A model — makes bots think

Without one, every `ai.generate` step returns its canned fixture text. Any
one of these in `~/.secrets/nanobots.env` is enough:

```sh
ONECLAW_API_KEY=...     # best: per-agent budget, PII redaction, injection screening
ANTHROPIC_API_KEY=...   # or
OPENAI_API_KEY=...      # or (any chat-completions gateway, with OPENAI_BASE_URL)
GEMINI_API_KEY=...      # or
```

Restart `nanobotd` after editing the file; it is read at startup only, and
is never written into this repo, logged, or handed to a bot container.
Settings shows which backend is live and whether guardrails are on. Which
backend serves which bot, and what you give up with a direct provider key,
is [llm.md](llm.md).

## 2. 1Claw — holds the credentials

`ONECLAW_API_KEY` does double duty: it is a model backend *and* the vault
every OAuth account (Google, X, LinkedIn) lands in — those still need it,
full stop. A static token (Slack, GitHub, Stripe, HubSpot) doesn't: without
`ONECLAW_API_KEY`, nanobotd falls back to an encrypted local file for those
by default (an OS keychain if you ask for one with
`NANOBOTS_SECRETS=keychain`) — see [secrets.md](secrets.md). So "a model, no
1Claw" is enough to run a GitHub- or Slack-backed bot for real; it's Gmail,
Drive, X and LinkedIn that stay off without this layer.

`nanobots init` writes this file for you and will enrol an agent key rather
than asking for full account access — the difference between the two kinds
of key, and why the narrower one is the default, is [setup.md](setup.md).

One thing to watch: this repo gives **every distinct bot name its own 1Claw
agent**, and plans cap how many an account can hold. Settings → System →
Posture shows the count against your plan (`25/50 agents`) and turns amber
at 80%. Past the cap, a run that needs a new bot fails with
`Agent limit reached` ([oneclaw-bridge.md](oneclaw-bridge.md)).

## 3. Connect an account — makes bots act

All of this happens in **Settings → Connect a service**. Two kinds, very
different effort. The per-provider walkthroughs are in
[connections.md](connections.md); this is the summary.

**Paste a token.** No OAuth app, about a minute each:

| Provider | Where to get it | Scope |
|---|---|---|
| GitHub | a personal access token | `repo`, or `public_repo` for public repos only |
| Slack | api.slack.com/apps → bot token (`xoxb-…`) | `chat:write` |
| Stripe | dashboard.stripe.com/apikeys | a secret key |
| HubSpot | a private app token | `crm.objects.contacts.read`, `.write` |

**Register your own OAuth app.** Longer, and the client id goes in
`~/.secrets/nanobots.env` before the Connect button will do anything:

| Provider | Env var | Scopes it asks for |
|---|---|---|
| Google | `GOOGLE_OAUTH_CLIENT_ID` | Gmail read/send/compose/modify, Drive file+readonly, Calendar readonly |
| X | `X_OAUTH_CLIENT_ID` | `tweet.read`, `tweet.write`, `users.read`, `offline.access` |
| LinkedIn | `LINKEDIN_OAUTH_CLIENT_ID` | `openid`, `profile`, `w_member_social` |

No client secret for Google or X: both use OAuth2 + PKCE as a public client
(`internal/oauth2pkce`). LinkedIn is the exception and needs
`LINKEDIN_OAUTH_CLIENT_SECRET` too.

**Register it as a "Desktop app" / native client.** The redirect is a
loopback URL on an **ephemeral port** — `http://127.0.0.1:<random>/` —
because the flow starts a throwaway local listener. Google's Desktop-app
client type accepts any loopback port, which is the path this build is
tested on. A provider console that demands one exact redirect URI does not
fit that, and X and LinkedIn have not been taken through registration here —
treat those two as implemented but unverified end to end.

1Claw's connector presets (`nanobots connectors`,
[connectors.md](connectors.md)) do **not** remove this step. 1Claw ships no
shared OAuth apps, so you still register your own app once — with 1Claw
rather than with this repo.

## What each swarm needs

Connecting nothing is fine; those swarms run on fixtures. This is the cost
of making each one real:

| Swarm | Needs connected | Your own OAuth app? |
|---|---|---|
| `supervisor-review` | nothing | no |
| `github-digest-to-slack` | github, slack | no |
| `daily-email-recap` | google | google |
| `inbox-autopilot` | google | google |
| `listen-and-reply` | x | x |
| `never-drop-a-thread` | google | google |
| `weekly-client-report` | google | google |
| `bookkeeping-assistant` | google, slack | google |
| `daily-inbox-recap` | google, slack | google |
| `meeting-to-action` | google, slack | google |
| `morning-brief` | google, slack | google |
| `support-desk-lite` | google, slack | google |
| `get-paid` | google, slack, stripe | google |
| `lead-to-meeting` | google, hubspot, slack | google |
| `content-engine` | linkedin, x | linkedin, x |
| `repurpose-everything` | google, linkedin, x | google, linkedin, x |

`supervisor-review` needs nothing because it only reasons.
**`github-digest-to-slack` is the cheapest real automation**: two pasted
tokens, no OAuth app, and it is already on a weekday-morning cron. Google is
the widest unlock: one OAuth app turns on twelve of the sixteen.

`review-responder` declares `google_business_profile`, for which no client
exists yet; that bot stays on fixtures whatever you connect
([connections.md](connections.md)).

## Checking it worked

```sh
nanobots conform bots     # every bot against its fixtures
nanobots plan             # every swarm type-checks
nanobots webhook <swarm>  # the URL and token for a webhook swarm
nanobots spend            # what the models have cost
```

A run tells you which services were fixtures and which were real: the run
detail page shows a **DEMO DATA** banner naming them, and each log line says
`-> ok (demo data — not your real google)` when it was a fixture. A
convincing fake that looks exactly like a real result is the worst thing
this product could quietly do, so it never does it silently.

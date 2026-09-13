# Connecting a real account

A bot reaches a real Gmail or Slack through a **binding** on its 1Claw
agent. There are two ways to get one, and the honest summary is that both
need you to register an OAuth application with the provider — the choice is
about who runs the round trip and holds the secret, not about avoiding the
provider's console.

## What 1Claw does and does not give you

1Claw ships a catalogue of connector presets — `nanobots connectors list`:

```
gmail  google-sheets  google-calendar  github  slack  x  discord  notion  honcho
```

Each preset carries the provider, the scopes to request, and the host
guardrails the binding executes under. What it does **not** carry is a
shared OAuth application. Installing one before registering an app answers:

```
No app credentials configured for 'Google'. Register your OAuth app
credentials first.
```

That was worth finding out by trying rather than assuming: the presets look
like one-click integrations and are not. `honcho` is the exception, because
it takes a pasted API key rather than an OAuth round trip.

## The flow

```sh
# 1. Register your provider app, once. The secret goes to 1Claw and is
#    never written to disk here.
nanobots connectors register inbox-triage google <client-id> <client-secret>

# 2. Install the connector — creates the binding, returns a URL.
nanobots connectors install inbox-triage gmail

# 3. Open the URL, grant access.
nanobots connectors status inbox-triage      # "connected" once you have
```

Then set that bot's service to `connection: oauth_1claw` and it executes
against the binding.

The binding name matters: `LiveDeps.ServiceCall` executes against a binding
named after the bot's own service id, so a bot declaring
`services: [{id: gmail}]` needs the binding called `gmail`. That is the
default; `--as <name>` overrides it.

This closes `internal/step`'s oldest TODO, which said binding provisioning
"isn't built yet — this assumes a binding named svc.ID already exists on
the agent". Installing a connector is what creates one.

## Or nanobots' own OAuth clients

`internal/google`, `internal/x` and `internal/linkedin` implement OAuth
directly — `nanobots connect google` and the Settings page. Same provider
console, same client id; the difference is that nanobots holds the refresh
token in your 1Claw vault and runs the flow itself.

Which to use:

- **1Claw connectors** for a provider this repo has no client for. Register
  once, and every future provider is a preset rather than a new Go package
  and another refresh-token dance.
- **The built-in clients** for Google, X and LinkedIn, which already exist,
  are tested against fake servers, and support the per-bot
  `connection: demo` → live switch in Settings.

## Installed is not connected

An install creates the binding; the binding is unusable until the browser
round trip finishes. `status` reports the two separately, because
conflating them is how a bot ends up failing on a credential that looks
present.

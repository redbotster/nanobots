# Starting a swarm from outside

`trigger: {type: webhook}` has been in the schema since the first commit,
reported by `/api/swarms`, and inert. `lead-to-meeting` is written around
one — a website form is submitted, a lead gets logged, enriched and routed —
and the only way to run it was by hand.

```sh
curl -X POST http://127.0.0.1:7474/webhooks/lead-to-meeting \
  -H "Authorization: Bearer $(cat ~/.nanobots/state/agents/webhook-token)" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Priya Raman","email":"priya@meridian.example","company":"Meridian Freight"}'
# → 202 {"run_id":"…","swarm":"lead-to-meeting","status":"running"}
```

The body becomes `{{trigger.payload}}` in every bot's input templates,
alongside `{{vars}}` and `{{run.date}}`.

## One template, two ways to run

```yaml
vars:
  example_lead: { name: "Dana Whitfield", email: "dana@northwind.example" }

bots:
  - id: intake
    use: form-to-sheet@0.1.0
    inputs:
      payload: "{{trigger.payload | default: vars.example_lead}}"
```

`default:` now falls back to another path when that path resolves, and to a
literal when it doesn't. That small generalisation is what makes this work:
a literal cannot carry a JSON object, so without it a webhook swarm is
either unrunnable by hand or carries a fake lead in its own inputs forever.
Existing fallbacks are untouched — `{{memory.last_run_at | default: now-24h}}`
resolves to nothing and stays the string it always was.

An empty value falls through the same as a missing one, because
`trigger.payload` is present on every run and empty on the ones no webhook
started. A `false` or a `0` is a real value someone chose, and stays.

## What it refuses

- **No token, no run.** This endpoint starts runs and runs send email; an
  unauthenticated URL that emails your customers is not a feature. The
  token is minted on first start and lives at
  `~/.nanobots/state/agents/webhook-token`, `0600`. A `?token=` query
  parameter also works, because many form services cannot set headers —
  that is the weaker option and it is the caller's choice, not a default.
- **A swarm that didn't ask for it.** Posting to a cron swarm is a 409
  naming its actual trigger. Otherwise the token becomes a "run anything"
  key and a nightly swarm could be fired at 3am by whoever holds it.
- **A body that isn't JSON.** Refused up front rather than passed through
  as a string — a bot declaring a `json` input would get something it
  cannot read, and "your form posted form-encoded" is a better error than a
  type failure three containers in.

It answers **202, not 200**: the run has started, not finished. A sender
should not be held open while a swarm waits half an hour at an approval
gate.

## Verified

A real POST to a running daemon started `lead-to-meeting`, and the entry
bot's container received exactly the posted lead in its `inputs.json` —
`Priya Raman`, `Meridian Freight` — rather than the example. Checking the
run's *output* would have proved nothing: `form-to-sheet` is on
`connection: demo`, so its sheet append returns the same canned row
whatever you post.

# What a bot and a swarm actually look like

Both are YAML you can read. The two below are real files in this repo, not
illustrations — `internal/contract`'s `TestTheDocsQuoteTheRealFiles` fails if
they drift from what is quoted here.

## A nanobot

A bot declares typed ports and a fixed list of steps. Nothing else in the
system needs to know how it works:

```yaml
  ports:
    inputs:
      - name: repo
        type: string
        required: true
      - name: max_issues
        type: string
        default: "10"
      - name: instructions
        type: string
        required: false
        description: "How you want this bot to work — tone, priorities, wording. Shapes how it does its job, never what it is allowed to do."
        default: "Lead with anything blocking a release. Group by area, not by date."
    outputs:
      - name: digest
        type: string

  steps:
    - name: fetch
      type: service.call
      service: github
      op: issues.list
      params: { repo: "{{inputs.repo}}", max: "{{inputs.max_issues}}" }
    - name: summarise
      type: ai.generate
      prompt_file: ./prompts/digest.md
      inputs: { issues: "{{steps.fetch.output}}", instructions: "{{inputs.instructions}}" }
      outputs:
        digest: "{{steps.summarise.output.digest}}"
```

That is `bots/github-issues-digest/nanobot.yaml`. `service.call` and
`ai.generate` are callbacks to `nanobotd`, so the container never sees a
GitHub token or a model key ([architecture.md](architecture.md)).

`instructions` is the customisation port every LLM bot carries: it shapes
how a bot does its job, never what it is allowed to do, and it is editable
from the bot's card in the UI or overridable per swarm. The precedence rule
lives in one place in Go rather than in nineteen prompt files
([bot-contract.md](bot-contract.md)).

## A nanoswarm

A swarm says which bots to run and how to wire them. A `snap` connects one
bot's output port to another's input port, and the planner refuses the swarm
if the types do not match:

```yaml
apiVersion: nanobots.dev/v1alpha1
kind: Nanoswarm
metadata:
  name: github-digest-to-slack
  description: Every weekday morning, summarise a repo's newest open issues and post the digest to Slack.
  owner: me@example.com

spec:
  defaults:
    model: { provider: anthropic, name: claude-sonnet-4-6 }
    guardrails:
      pii: allow
      injection_threshold: 0.7
      daily_budget_usd: 5
    resources: { preset: small }

  vars:
    repo: "redbotster/nanobots"
    notify_channel: "slack:#eng"

  trigger:
    type: cron
    expr: "0 8 * * 1-5"
    timezone: America/Chicago

  bots:
    - id: digest
      use: github-issues-digest@0.1.0
      inputs:
        repo: "{{vars.repo}}"
        max_issues: "10"
    - id: notifier
      use: notify@0.1.0
      inputs:
        channel: "{{vars.notify_channel}}"
      # A notification is the last thing this swarm does and nothing
      # reads it. Losing it should not fail a run whose real work already
      # succeeded — see docs/error-policy.md.
      on_error: continue

  snaps:
    - from: digest.digest
      to: notifier.message

  deploy:
    target: local
```

That is `examples/swarms/github-digest-to-slack.yaml`, and it is the
cheapest real automation in the catalog: two pasted tokens, no OAuth app,
already on a weekday-morning cron ([going-live.md](going-live.md)).

## The parts worth knowing

| Field | What it decides |
|---|---|
| `bots[].use` | which `bots/<id>` at which version, resolved by the planner |
| `bots[].inputs` | literal values and `{{vars.*}}` for ports nothing snaps into |
| `bots[].on_error` | `fail` (the default) or `continue`, which skips everything downstream ([error-policy.md](error-policy.md)) |
| `snaps` | `from: <bot>.<port>` to `to: <bot>.<port>`, type-checked; `.field` drills into a `json` port, `.*` fans out over a list ([fan-out.md](fan-out.md)) |
| `trigger` | `manual`, `cron` with a timezone ([scheduler.md](scheduler.md)), `webhook` ([webhooks.md](webhooks.md)), or `event` |
| `defaults.guardrails` | budget, PII policy and injection threshold, applied per bot |

## Running it for real

```sh
nanobots plan -f examples/swarms/github-digest-to-slack.yaml   # type-check, print the DAG
nanobots run  -f examples/swarms/github-digest-to-slack.yaml   # run it, printing the log
```

Or open it on the canvas in the app, which runs the same planner live as you
edit ([builder.md](builder.md)).

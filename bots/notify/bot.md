# notify

Post a message to Slack, email, or SMS.

`bare` harness, one step: send `inputs.message` to `inputs.channel`, return whether it was delivered. The `channel` string names both the medium and destination together (e.g. `"slack:#ops"`, `"email:me@example.com"`) — this bot doesn't parse it, it just passes it through to whatever `Deps.Notify` implementation is behind it.

TODO(nanobots#notify-backend): no real Slack/email/SMS delivery is wired up in this build — `Deps.Notify` is a no-op that always reports delivered, in both demo and live modes (see internal/step/demo.go, internal/step/live.go).

# notify

Post a message to Slack, email, or SMS.

`bare` harness, one step: send `inputs.message` to `inputs.channel`, return whether it was delivered. The `channel` string names both the medium and destination together (e.g. `"slack:#ops"`, `"email:me@example.com"`) — this bot doesn't parse it, it just passes it through to whatever `Deps.Notify` implementation is behind it.

A `"slack:..."` channel is real in live mode: `internal/step/slack_live.go` posts to that channel for real, using a bot token connected once from the WebUI's Settings page (or `nanobots connect slack`), stored in a 1Claw vault secret. `"email:..."`/`"sms:..."` channels (and Slack itself, in demo mode) still have no live backend — `Deps.Notify` degrades to a no-op that reports delivered, a disclosed gap rather than a silent success (see `internal/step/demo.go`).

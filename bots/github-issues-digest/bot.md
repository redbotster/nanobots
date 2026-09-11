# github-issues-digest

Summarise a repo's newest open issues into a short plain-text digest.

`openclaw` harness (needs `ai.generate`), two steps: `service.call` fetches up to `inputs.max_issues` open issues from `inputs.repo` (an `"owner/name"` string), then `ai.generate` turns them into a short, chat-message-ready digest.

Real live behavior: the `github` service (`internal/step/github_live.go`) hits the real GitHub REST API once a personal access token is connected (WebUI Settings, or `nanobots connect github`) and the service's `connection:` is switched from `demo` to any non-demo value. Ships on `connection: demo` by default — see `docs/connections.md`.

Pull requests are filtered out of the fetch (GitHub's API returns them alongside issues) — this bot only ever digests actual issues.

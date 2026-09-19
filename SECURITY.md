# Security

## Reporting a vulnerability

Please report security issues privately, through GitHub's
[private vulnerability reporting](https://github.com/redbotster/nanobots/security/advisories/new)
on this repository, rather than opening a public issue.

Include what you did, what happened, and what you expected. A minimal
reproduction — a `curl` line, a swarm YAML, a bot manifest — is worth more
than a description, because most of this project's real defects have been
found by running something rather than by reading it.

## What this software touches

Worth knowing before you assess it. `nanobotd` is a local daemon that:

- binds `127.0.0.1` and serves an unauthenticated REST API to anything on the
  loopback interface, including other processes on the same machine;
- exposes two token-authenticated routes that are reachable from Docker
  containers through `host.docker.internal` — `POST /shroud/…`, which spends
  money on model calls, and `POST /webhooks/{swarm}`, which starts runs;
- runs bots as Docker containers with a bind-mounted working directory;
- holds a 1Claw Human API key in process memory, and reads and writes vault
  secrets with it;
- performs `web.fetch` requests **in the daemon**, from URLs supplied in a
  bot's inputs.

Credentials live in `~/.secrets/nanobots.env` and per-agent keys under
`~/.nanobots/state/agents/`. Neither is ever committed, logged, or passed
into a bot container.

## Things already known and deliberate

Please don't report these as vulnerabilities; they are documented decisions,
and saying so here saves you the time:

- **The loopback API has no authentication.** This is a local-first,
  single-user daemon. Anything that can reach `127.0.0.1:7474` is already
  running as you. The two routes that spend money or send mail are the
  exceptions and do carry tokens.
- **Bots run with network egress.** A bot declaring `network_egress` reaches
  the hosts on that list from inside its container, and nothing outside it —
  see below.
- **Demo mode is the default.** Every bot ships on `connection: demo` and
  reads fixtures, not your accounts, until you change it.

## Hardening already in place

Reported findings should take these into account:

- `web.fetch` refuses loopback, link-local (including cloud instance
  metadata), private, multicast and unspecified addresses. The check runs in
  the dialer's `Control` hook, on the address actually being connected to, so
  DNS rebinding does not get past it (`internal/step/webfetch_guard.go`).
- A container's own network is limited to `guardrails.network_egress`, not
  just its callbacks to nanobotd. An `openclaw` bot renders HTML in a real
  Chromium inside its container; a forward proxy (`internal/runner.EgressProxy`)
  is the only route out for it, checking the same allowlist `web.fetch` does,
  so a remote asset a rendered page references can't reach further than a
  `service.call` could. A bot declaring no egress gets no network interface
  at all (`--network none`).
- Path segments from a URL are validated before becoming filesystem paths.
  `http.ServeMux` routes on the escaped path while handlers read the decoded
  one, so `..%2f` is not what routing saw — see `internal/api/botpath.go`.
- CORS echoes only loopback origins, parsed rather than prefix-matched.
- The blob store accepts only a 64-character hex digest.
- Approval prompts state what a bot may write to before you answer.

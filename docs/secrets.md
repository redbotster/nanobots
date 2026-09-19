# Secrets

Every real credential nanobots has ever held — a Slack bot token, a GitHub
personal access token, a Google refresh token — has lived in exactly one
place: a 1Claw vault, reached from `internal/step.vaultToken` straight
through `internal/oneclaw.Client.GetSecret`/`PutSecret`. That meant a real
connection needed a 1Claw account even for the two-minute version: paste a
token, run the bot.

`internal/secrets` is the fix for that. GitHub, Slack, Stripe and HubSpot's
static tokens — the four providers that were only ever a paste, never an
OAuth dance — now read and write through a `Store` chosen once at startup:
`internal/step/vault_token.go` and the paste-a-token handlers in
`internal/api/connections.go` no longer touch `internal/oneclaw` for these
directly. A machine with only `ANTHROPIC_API_KEY` and a pasted GitHub token
now runs a GitHub-backed bot live with no 1Claw account.

What's still 1Claw-only: Google, X and LinkedIn are OAuth refresh-token
flows, and both connecting them (`internal/api/connections.go`'s
`handleConnect{Google,X,LinkedIn}Start`) and reading them back
(`internal/step/{google,x,linkedin}_live.go`) still go straight through
`*oneclaw.Client`. Moving those off 1Claw is a real OAuth-callback
redesign, not a drop-in `Store` swap, and hasn't been attempted. See
[oneclaw-bridge.md](oneclaw-bridge.md) for how the vault is reached for
those and for everything else 1Claw still does (agents, memory, approvals).

## The interface

```go
type Store interface {
    Get(key string) (value string, found bool, err error)
    Put(key, value string) error
    Delete(key string) error
    Describe() string
}
```

One small interface, several backends — the same shape
[llm.md](llm.md) and [memory.md](memory.md) already use for the identical
reason: a capability that used to mean "1Claw or nothing."

Every backend is keyed by a flat string — `"slack/bot_token"`,
`"github/token"` — the same path convention the 1Claw vault already uses,
so a caller doesn't need to know which backend answered.

## The three backends

| Backend | Where the secret actually lives | What protects it |
|---|---|---|
| `secrets.OneClaw` | 1Claw's vault | 1Claw's own encryption, access log, and passkey policy |
| `secrets.Keychain` | the OS credential store (macOS Keychain, Secret Service, Windows Credential Manager) | whatever protects your OS login |
| `secrets.File` | a file on this machine, age-encrypted | this machine's own file permissions |

**`OneClaw`** wraps the existing `*oneclaw.Client` and a vault ID. `Delete`
returns an error rather than pretending to succeed: 1Claw's vault API
(verified against the real spec, see
[1claw-feature-requests.md](1claw-feature-requests.md)) has no endpoint to
delete a single secret, only a whole vault — which this repo will never do
on someone's behalf.

**`Keychain`** shells out to the OS (via `github.com/zalando/go-keyring` —
the `security` CLI on macOS, not cgo). **It is opt-in only**
(`NANOBOTS_SECRETS=keychain`) — never selected automatically, for a reason
found the hard way, not assumed. It does not always work, and two different
failure shapes have both been reproduced live in this repo, not guessed at:

- In a sandboxed, non-interactive shell, `keyring.Set` fails fast:
  ```
  security: SecKeychainItemCreateFromContent (<default>): User interaction is not allowed.
  ```
  That's a non-interactive session correctly being refused — CI, a
  container, exactly the kind of environment `nanobotd` might run in.
- In a session that *can* show UI, it doesn't fail — it can pop a real,
  silent macOS permission dialog and then block waiting for a human to
  click it. Run inside this repo's own `go test ./... -race`, with no 1Claw
  configured, the original design (which tried the keychain automatically
  whenever no backend was chosen) hit exactly this: the whole `internal/daemon`
  package hung for the full ten-minute `go test` timeout before failing.
  That is what "automatic" would have meant for `nanobotd`'s own unattended
  startup too.

Both findings are why `secrets.Available()` — the real throwaway
set-then-delete probe a caller uses to check before trusting this backend,
rather than trusting the platform name — is bounded by a two-second
timeout and fails closed (reports unavailable) rather than hanging, and why
the backend is never chosen without a human asking for it by name.

**`File`** generates an [age](https://age-encryption.org) X25519 identity
once, at `secrets.age-key`, and stores every secret encrypted at
`secrets.age`, both files at mode `0600` next to each other — the same
trust boundary `~/.secrets/nanobots.env` already has for the 1Claw key
itself ([setup.md](setup.md)). This is the one backend proven to work in
every environment this repo has actually tried it in, including this one:
it needs nothing from the OS, only a directory it can write to.

What it does and doesn't protect against, stated plainly: it protects a
secrets file that ends up somewhere with looser permissions by accident — a
backup, a synced folder, a `tar` that didn't preserve modes — or gets read
by something that can see file contents but not permission metadata. It
does **not** protect against another process running as the same user,
which could read the key file exactly as nanobotd does. A
passphrase-protected key would close that gap and isn't built yet: it needs
an interactive prompt somewhere in `nanobots init` or `nanobotd`'s startup,
which doesn't exist for this backend today.

## Choosing a backend

`internal/wiring.BuildSecretsStore` picks one backend at startup, for the
whole process:

- `NANOBOTS_SECRETS=oneclaw|keychain|file` picks explicitly. `oneclaw`
  fails loudly if `ONECLAW_API_KEY` isn't set, rather than silently
  falling back to something else.
- Otherwise, a configured 1Claw account is the default — it already has a
  vault, and may already hold a token connected through `nanobots connect`
  or Settings, so using it avoids silently splitting one install's
  credentials across two places.
- Without one, the default is `File` — **never** an automatic keychain
  probe. That was the original design, and it was wrong: reproduced live
  in this repo's own test suite, trying the keychain automatically hung
  `internal/daemon`'s tests for the full ten-minute `go test` timeout,
  because the probe can pop a real permission dialog and then wait
  forever for a human who isn't there. `Keychain` is opt-in only, via
  `NANOBOTS_SECRETS=keychain`.

Whichever backend is chosen, only GitHub/Slack/Stripe/HubSpot use it.
Google/X/LinkedIn still always go through 1Claw, as above.

## What's next

A Settings-page line stating which backend holds a given credential and
what that implies doesn't exist yet — the WebUI's connect flow works with
any backend now, but doesn't yet say which one it just wrote to. Moving
Google/X/LinkedIn off 1Claw, if that's ever wanted, needs a real
OAuth-callback redesign, not a `Store` swap — see the note above.

## Run it for real

Every claim on this page is backed by a test that does the real thing, not
a mock of it — `TestFileStoreOnDiskIsNotThePlaintext` actually reads the
file `File.Put` wrote and checks the plaintext isn't in it;
`TestKeychainRoundTripsWhenAvailable` actually calls the OS keychain and
skips, rather than fakes success, when the OS refuses:

```sh
go test ./internal/secrets/... -v
```

To see the encryption yourself: write a secret with `File`, then look at
what's actually on disk. It has to live inside the module — `internal/secrets`
follows Go's own rule that an internal package is only importable from
within its module tree, so a one-off `/tmp` script can't reach it.

```sh
mkdir -p tmp-secrets-demo && cat > tmp-secrets-demo/main.go <<'EOF'
package main

import (
	"fmt"
	"github.com/redbotster/nanobots/internal/secrets"
)

func main() {
	f := &secrets.File{Dir: "/tmp/secrets-demo"}
	must(f.Put("github/token", "ghp_this_is_not_actually_a_real_token"))
	v, found, err := f.Get("github/token")
	must(err)
	fmt.Println("decrypted back:", v, found)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
EOF
go run ./tmp-secrets-demo
grep -c ghp_this_is_not_actually_a_real_token /tmp/secrets-demo/secrets.age  # 0 — not on disk in the clear
file /tmp/secrets-demo/secrets.age  # "data" — age-encrypted binary, not JSON
rm -rf tmp-secrets-demo /tmp/secrets-demo
```

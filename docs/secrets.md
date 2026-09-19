# Secrets

Every real credential nanobots has ever held — a Slack bot token, a GitHub
personal access token, a Google refresh token — has lived in exactly one
place: a 1Claw vault, reached from `internal/step.vaultToken` straight
through `internal/oneclaw.Client.GetSecret`/`PutSecret`. That meant a real
connection needed a 1Claw account even for the two-minute version: paste a
token, run the bot.

`internal/secrets` is the fix for that, and this page is honest about how
much of it is built. **The interface and its backends exist and are fully
tested. Nothing in the running app uses them yet** — the connect-flow
handlers in `internal/api` and `internal/step/vault_token.go` still go
straight to `internal/oneclaw` directly. This page describes the part that
exists; it does not claim the "no 1Claw account" story works end to end,
because it doesn't yet. See [oneclaw-bridge.md](oneclaw-bridge.md) for how
the vault is actually reached today.

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
the `security` CLI on macOS, not cgo). It does not always work, and
`secrets.Available()` is how you find out *before* trusting it rather than
after a `Put` fails: verified live in this repo's own sandboxed dev shell
that `keyring.Set` fails there with

```
security: SecKeychainItemCreateFromContent (<default>): User interaction is not allowed.
```

A non-interactive session — CI, a container, a sandboxed shell, exactly the
kind of environment `nanobotd` itself might run in — cannot unlock or write
to a login keychain at all. That's the OS doing its job, not a bug in the
wrapper. `Available()` does a real throwaway set-then-delete rather than
trusting the platform name, because the platform name is exactly what makes
this look available when it isn't: this build compiles the macOS backend on
every macOS host, whether or not the current session can pass that gate.

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

## What's next

Wiring `internal/step/vault_token.go` and the `internal/api` connect-flow
handlers through `Store` instead of `*oneclaw.Client` directly, a
`NANOBOTS_SECRETS=` backend-selection env var in `internal/wiring`, and a
Settings-page line stating which backend holds a given credential and what
that implies — none of that exists yet. Until it does, every real
connection in this app still goes through the 1Claw vault, as
[oneclaw-bridge.md](oneclaw-bridge.md) describes.

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

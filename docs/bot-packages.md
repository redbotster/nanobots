# Bot packages

A bot is a self-contained directory: `nanobot.yaml`, `bot.md`, `fixtures/`,
and whichever of `prompts/`, `schemas/`, `templates/` it needs. Every
internal reference inside those files is relative to the bot's own
directory — nothing in the catalog reaches outside itself, with one
disclosed exception below. That's what makes a bot a package rather than
just a file in a shared directory: it's the whole unit a `use: name@version`
reference names.

## How `use: name@version` resolves today

One place, `internal/botpkg.Source` (`internal/botpkg/botpkg.go`), and one
implementation of it, `LocalDir`: walk to `<bots dir>/<name>/nanobot.yaml`,
and if a version was requested, check it against `metadata.version` in that
file — a plain string-equality check, not semver. That's the whole
algorithm, and until now it was reimplemented separately everywhere a bot
was loaded — the planner, the WebUI's bot listing, `nanobots import`,
foundry's promote step — and only the planner's copy actually checked the
version.

That mattered in a concrete, shipped way: `nanobots import`'s own copy
(`cmd/nanobots/share.go`'s `catalogLookup`) discarded everything after the
`"@"` and loaded whatever version happened to be on disk. A bundle naming a
newer `invoice-chaser` than the one installed imported cleanly —
*"you have everything this bundle needs"* — and the real mismatch surfaced
later, confusingly, at `plan` or `run`. Both paths now go through the same
`botpkg.Source`, so the answer can't disagree with itself depending on
which command you ran. See [sharing.md](sharing.md) for what that error
looks like from `nanobots import`.

## What "no registry yet" actually means

Only one version of any bot can exist in a `LocalDir` at a time — it's a
flat directory, `bots/<name>/`, not `bots/<name>/<version>/`. So version
"pinning" today is a hard refusal, never a choice among installed versions:
`use: notify@0.1.0` either matches what's on disk or the swarm doesn't
resolve. There is nowhere yet to actually range over `0.1.0` and `0.2.0`
side by side, and no code anywhere in this repo does semver comparison,
ranges, or "latest" — `grep -rn "semver"` in `internal/` finds nothing,
deliberately, rather than a half-built version resolver nothing exercises.

`Source` is built as an interface specifically so that changes when there's
somewhere to fetch a bot *from* — a `RemoteSource` implementing the same
two methods, `Resolve` and `List` — without every caller's resolution logic
changing again. That's additive, not a rewrite, and it doesn't exist yet
because there is no registry to fetch from: standing one up is a hosting
and distribution decision, not a code change, and this repo doesn't build
speculative infrastructure for a service that isn't there (see
[1claw-feature-requests.md](1claw-feature-requests.md) for the house style
on disclosed gaps like this one).

## The one bot that isn't fully self-contained

`review-board`'s `roster` input defaults to `{{roles.roster}}`
(`bots/review-board/nanobot.yaml`), populated at run time from a shared,
engine-owned `roles/roles.yaml` at the repo root plus any user overrides in
`~/.nanobots/state/roles.json` (`internal/roles`, [supervisors.md](supervisors.md)).
Every other bot in the catalog resolves entirely from its own directory.
This is a template value the engine injects, not a file the bot reads
directly, so it doesn't change what "a bot is a self-contained directory"
means for packaging — but it's the one place a bot's actual behavior
depends on something outside itself, and pretending otherwise would be the
kind of claim this repo's own rules exist to catch.

## Run it for real

The version check is real and refuses a real mismatch, not a mocked one:

```sh
nanobots export -f examples/swarms/github-digest-to-slack.yaml -o /tmp/bundle.yaml
sed -i '' 's/github-issues-digest@0.1.0/github-issues-digest@9.9.9/' /tmp/bundle.yaml
nanobots import /tmp/bundle.yaml
# error: this bundle needs bots you do not have: github-issues-digest@9.9.9
# nothing was written — add them to bots/ and import again
```

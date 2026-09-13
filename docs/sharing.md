# Handing a swarm to someone else

A swarm you build is stuck on the machine that built it. The catalog was
the only way to get one, which is a strange gap in a product whose whole
shape is "snap these bricks together".

```sh
nanobots export -f examples/swarms/get-paid.yaml -o get-paid.bundle.yaml
# → wrote get-paid.bundle.yaml — 3 bot(s), can write to gmail

nanobots import get-paid.bundle.yaml
# → imported examples/swarms/get-paid.yaml
#     when it runs it can write to: gmail
#   check it before running:  nanobots plan -f …
```

Export with no `-o` writes to stdout, so it pipes.

## What a bundle contains

The swarm file **verbatim** — comments and all. Every catalog swarm's
header explains the choices it made, and that is most of what a reader
needs; re-marshalling the structure would throw it away.

Plus what a recipient needs to know before running it:

- **`requires`** — every catalog bot, as `id@version`.
- **`connects`** — the accounts it will want, derived from the bots rather
  than declared, so a bundle cannot be wrong about its own swarm. A
  demo-only swarm asks for nothing, because that is the point of demo mode
  and listing it would make every bundle look like it wants your Gmail.
- **`acts`** — what it can write to when it runs.

## Bots are named, not carried

They are catalog entries with versions. Shipping copies would fork them
silently — you would run *a* `invoice-chaser`, not *the* one. A bundle that
names `invoice-chaser@0.1.0` and cannot find it says exactly that, and
imports nothing:

```
this bundle needs bots you do not have: invoice-chaser@0.1.0
nothing was written — add them to bots/ and import again
```

Everything is checked before anything is written. A swarm referencing a bot
you don't have is not importable, and finding that out at run time — after
it has been saved and looks legitimate — is the worse order.

A bot referenced by local `path:` cannot be exported at all: it lives only
on the machine that wrote it, and a bundle carrying that reference is
guaranteed to fail on arrival.

## It is executable, and says so

`get-paid` emails your customers. Handing someone a swarm is handing them a
script, and the bundle says so where they will see it — in the file header
in capitals, and again on import:

```
# WHEN RUN, THIS SWARM CAN WRITE TO: gmail
# Read it before running it, the same as any script someone sends you.
```

**A bundle carries no credentials.** Whoever imports it connects their own
accounts (`docs/connectors.md`). What it can carry is whatever is in the
swarm's own `vars:` and `owner:` — a channel name, an email address — so
read your own bundle before sending it, the same as any file.

# `when:`, and the one thing it's allowed to look at

`get-paid` reminds every overdue account the same way. The obvious next
thing someone wants is to reminder differently past a threshold — ask
before sending anything over $500, skip the polite nudge for an account
that's ninety days out. Today that means writing a second bot, or a step
inside one bot that quietly does nothing on the branch nobody's looking at.
`when:` is the first alternative: a condition on a bot **instance**, decided
before it runs.

## The policy

```yaml
bots:
  - id: escalate
    use: send-escalation-email@1.0.0
    when: "{{inputs.amount}} > 500"
    inputs:
      amount: "{{invoice.amount}}"    # a snap, most of the time
```

Empty (the default) always runs — same convention as `on_error`, which
writes no key at all for its default rather than littering every swarm file
with `on_error: stop`.

## What it can look at, and why that's the whole rule

Only `{{inputs.<port>}}` — this bot's own resolved input, by the exact name
it declared. Not `vars`, not `trigger`, not another bot's id.

That looks narrow and is meant to. An "upstream port" only means anything to
`escalate` once it has arrived *through a snap*, onto a port `escalate`
itself declares — the same three sources (`inputs:`, a snap, a default)
every other input in this repo can come from. Allowing `when:` to reach past
that would parse a condition that reads like it's testing live data but is
actually testing a copy nobody wired up, the same silent-misread `stop.if`
and `on_error` were both written to avoid. If the value you want to gate on
hasn't reached this bot as an input yet, that's the thing to fix first —
snap it in.

## The comparison

`==`, `!=`, `<`, `<=`, `>`, `>=`, or no operator at all for a bare truthy
check (`when: "{{inputs.urgent}}"` — false for `""`, `nil`, `false`, `"0"`
or `"false"`, true for everything else). The set is deliberately the same
shape as 1Claw Automations' own `condition` step
(`{{steps.balance.output.native_balance}} < 0.01`), so a swarm and an
automation read alike.

A quoted side (`== "urgent"`, `== ""`) is taken literally rather than
resolved as a template — the only way to compare against the empty string,
since `== ` with nothing after it is refused as a missing value. An
unquoted side works exactly as before: `== overdue` compares to the literal
text `overdue`.

Two numbers compare as numbers even when one side arrived as a templated
string ("500" from a snap compares fine against a literal `500`). Two
strings only support `==`/`!=` — ordering two strings would silently answer
a question nobody asked, the same restriction `stop.if`'s `equals` already
lives under.

## What false actually does

Exactly what `on_error: continue` does to a failed bot: this instance
doesn't run, and **everything downstream of it is skipped too**, because its
outputs genuinely never arrived. It is not a failure — nothing is logged in
red, nothing is retried, and the run still succeeds. The log says which
condition and that it was false:

```
skipped — when: {{inputs.amount}} > 500 is false
```

## Caught at plan time

`nanobots plan` refuses:

- a reference to anything other than `{{inputs.<port>}}`
- a reference to a port this bot doesn't declare
- a comparison between two literals that aren't both numbers (`when: "true
  > false"` can never be true, on this run or any other — no different from
  an absurd retry count)

What it cannot refuse is whether the condition is actually true — that
needs a real run's data, same as every snap's actual value.

## What this is not

Not a way to branch a swarm's shape — every bot in the swarm is still
planned, typed, and shown on the canvas whether its `when:` turns out true
or false on a given run. Bounded iteration is `loop:` (`docs/loop.md`), a
separate primitive with its own restrictions. Nesting a whole swarm as one
node is `swarm:` (`docs/nested-swarms.md`), a separate, larger piece of the
same "control flow" idea.

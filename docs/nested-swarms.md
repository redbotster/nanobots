# `swarm:`, nesting a whole swarm as one node

A swarm can grow past what fits on one canvas — a refund flow with its own
lookup, its own drafting, its own approval, used the same way from three
different top-level swarms. Copying its bots into each one means fixing the
same bug three times later. `swarm:` is the other option: reference a whole
other swarm as a single node, typed like any other.

## The policy

The nested swarm declares its own boundary:

```yaml
# refund-flow.yaml
spec:
  ports:
    inputs:
      - name: customer_email
        type: string
        maps_to: lookup.email
    outputs:
      - name: summary
        type: string
        maps_to: writer.summary
  bots:
    - id: lookup
      use: refund-lookup@0.1.0
    - id: writer
      use: refund-writer@0.1.0
  snaps:
    - from: lookup.record
      to: writer.record
```

and a bot ref nests it exactly where `use:`/`path:` would go:

```yaml
# get-paid.yaml
bots:
  - id: refund
    swarm: ./refund-flow.yaml
    inputs:
      customer_email: "{{trigger.payload.email}}"
snaps:
  - from: intake.ticket_id
    to: refund.customer_email
  - from: refund.summary
    to: notifier.message
```

From `get-paid.yaml`'s point of view, `refund` is a node with one input
(`customer_email`) and one output (`summary`), exactly as if it were a
single bot. What actually runs behind it is `lookup` and `writer`, snapped
together, with `lookup`'s `email` fed from the outside and `writer`'s
`summary` handed back out.

## What `maps_to` means

Every `ports.inputs`/`ports.outputs` entry on the nested swarm names one
inner `<bot-id>.<port>` its value comes from or goes to. That's the whole
mechanism — a boundary port is not a new kind of port, it's a label on an
inner one. Only the ports listed here are reachable from outside; anything
the nested swarm's own bots wire up between themselves (like `lookup.record
-> writer.record` above) stays invisible to whoever nests it, the same way
a bot's own `steps:` are invisible to whoever snaps into its ports.

An inner input that isn't listed in `ports.inputs` needs its own value
inside the nested swarm's own YAML — a literal, a default, or a snap from
another bot in there. `refund-writer`'s `template` input, say, might always
be the same literal regardless of who nests this swarm, so it's set once
inside `refund-flow.yaml` rather than threaded through the boundary.

## How this actually works

**There is no new execution engine.** Before a swarm is resolved, every
`swarm:` reference is replaced with the referenced swarm's own bots and
snaps, entirely in memory — the planner's DAG, the runner's per-bot scheduling, retry,
`on_error`, `loop:`, `fallback:`, approvals, none of them ever learn that
nesting happened, because by the time any of them run, there is no nesting
left: `refund` becomes `refund/lookup` and `refund/writer`, two ordinary
bots, and the two boundary snaps above become ordinary snaps straight to
and from them. `nanobots plan`'s run order shows the real bots:

```
run order:
  intake
  refund/lookup
  refund/writer
  notifier
```

That naming is not cosmetic — it's how a run's log tells two nested
instances apart, and it's why an authored bot id can't contain `/` (or
`.`, which already separates a bot id from its port): both are reserved for
what this rewrite generates.

## What it refuses

- **A swarm with no declared `ports:`.** No boundary means nothing to type
  a snap against — the same reason an undeclared bot port can't be snapped.
- **Nesting a swarm that (directly or through others) nests the one nesting
  it.** Caught by tracking the chain of files being inlined; a self-nest
  through twenty levels is treated as a cycle path comparison missed rather
  than run until the stack gives out.
- **A `maps_to` that doesn't parse as `<bot-id>.<port>`**, or that names a
  bot or port the nested swarm doesn't actually have.
- **Combining `swarm:` with `retry:`, a non-default `on_error:`, `when:`,
  `loop:`, or `fallback:` on the same ref.** Once inlined, a nested swarm is
  several bots, not one — there's no single thing left for "retry this" or
  "loop this" to mean. Set those on the bots *inside* the nested swarm
  instead.
- **Fan-out into or out of a nested swarm's boundary.** `chaser.overdue.* ->
  refund.customer_email` doesn't type-check. Fan out a bot *inside* the
  nested swarm's own file instead, if that's what's needed there.

## What it doesn't check

A boundary port's declared `type:` isn't independently verified against the
inner port it maps to — once inlined, the rewritten snap goes through the
same type-check every other snap does, so a mismatch is still caught, just
in the renamed bot's own language (`refund/writer.summary is json, not
string`) rather than `refund-flow.yaml`'s. Read the error as "the port this
one maps to," not as a bug in the outer swarm.

## Verified

Run end to end in demo mode: an outer swarm with two bots, one of them
`swarm:`-nesting a one-bot inner swarm, its single input mapped through the
boundary. The run log names the inlined bot by its full path
(`refund/notify1`) and the run succeeds, with the mapped value reaching the
container exactly as a direct input would have.

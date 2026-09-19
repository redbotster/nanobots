# `loop:`, and why it isn't fan-out

Fan-out (`docs/fan-out.md`) answers "run this once per item in a list I
already have." It has no answer for "keep calling this API until it stops
handing back a next-page token" — the width isn't known before the bot
runs, because the bot itself is what discovers there's another page.
`loop:` is that second thing: bounded self-repetition, feeding a bot's own
output back into its own input.

## The policy

```yaml
bots:
  - id: fetch
    use: paginated-fetch@0.1.0
    inputs:
      url: "https://api.example.com/items"
      page_token: ""
    loop:
      max: 20
      feed:
        page_token: next_page_token
      until: '{{outputs.next_page_token}} == ""'
```

`fetch` runs, gets back `next_page_token: "abc123"`, and runs again with
`page_token: "abc123"` — everything else it was given (`url`) stays exactly
as it was. This repeats until `next_page_token` comes back empty or `max` is
reached, whichever happens first.

## The three parts

- **`max`** is required, and bounded to 1–20. An unbounded `while` against
  someone else's API is not a thing this runs unattended — a mistaken
  `until:` fails after twenty tries, not all night.
- **`feed`** maps this bot's own input port names to its own output port
  names. Both sides have to be ports the bot actually declares — there is
  nothing else to feed back, and a typo here would otherwise silently do
  nothing every single iteration.
- **`until`** stops the loop once true, checked after each iteration against
  that iteration's own `{{outputs.<port>}}` — the loop's mirror of `when:`'s
  `{{inputs.<port>}}`, same operators, same restriction to this bot's own
  ports. Empty (the default) means loop exactly `max` times.

## Comparing to an empty string

`{{outputs.next_page_token}} == ""` needs an actual empty string on the
right, not the two characters `"` `"`. A quoted side of a condition — on
`until:` or `when:` — is now taken literally, empty included, rather than
going through template resolution: `== ""` means "equals nothing", `==
"done"` means "equals the text done". This is what makes "stop once there's
no next page" expressible at all; there was previously no way to write
"equals nothing" in this language (`== ` with nothing after it is refused as
a missing value, and the un-quoted question never came up because nothing
had needed it yet).

## What downstream sees

**One of each output, not a list.** This is the deliberate difference from
fan-out, whose outputs become `list<T>` because a fanned-out bot's whole
point is that every item's result matters. A loop's point is usually the
opposite: only the *last* iteration's page token or status matters to
whoever reads this bot's output next. `nanobots plan` refuses a bot that
tries to be both — `loop:` and a fanned-out snap into the same instance
would disagree about which multiplicity downstream is typed against.

**Accumulating across pages is the bot's own job.** If what you actually
want is "all the items across every page", not just the last page's, the
bot's own steps write each page's items into memory and read the running
total back — `memory.get`/`memory.put`, the same primitive `drive-watch`
already uses to remember across separate *runs*, used here to remember
across iterations of one *run* instead (`docs/memory.md`). The orchestrator
staying out of this on purpose: it has no way to know which output on which
bot was meant to grow and which was meant to reset every time, and guessing
wrong would silently drop or duplicate data.

## What a failure does

A loop that fails on iteration 3 of 20 fails the run — no partial success,
no silent stop. `retry:` and `fallback:` still apply to a looping bot
instance exactly as they do to any other; they see the same interpreter that
loop iterations use, once. What they don't see is loop iterations already
completed: a retry re-runs the whole loop from iteration 1, not from where
it failed, for the same reason retrying any bot re-runs the whole thing —
see `docs/error-policy.md`.

## Caught at plan time

`nanobots plan` refuses:

- `max` outside 1–20
- a `feed:` entry naming a port this bot doesn't declare, on either side
- an `until:` that can't parse, or that reaches past this bot's own outputs
- `loop:` and a fanned-out snap on the same bot instance

What it cannot refuse is whether `until:` ever actually becomes true — a
loop with a condition that's never satisfied simply runs to `max` and stops
there, same as one with no `until:` at all.

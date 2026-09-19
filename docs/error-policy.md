# When a bot fails, does the run fail?

By default, yes. A swarm stops at the first bot that fails, and the run goes
red. That is the right default: silence should mean "tell me loudly".

It is the wrong answer surprisingly often. Running all fifteen catalog
swarms end to end, six failed on one thing — no Slack token in the vault —
and in every case the swarm had already done its real work. `get-paid` found
the overdue invoices, drafted the reminders, opened the approval gate, and
sent every one of them. Then it failed the run, because it could not post a
summary to a channel that nothing reads.

"The run failed" and "the notification didn't go out" are very different
sentences, and only one of them was true.

## The policy

```yaml
bots:
  - id: notifier
    use: notify@0.1.0
    on_error: continue    # stop (default) | continue
```

Per bot **instance**, not per bot. Only the swarm knows whether a failure
matters: `notify` failing at the end of `get-paid` is a missed Slack
message, while the same bot elsewhere might be the whole point of the swarm.
A bot cannot make that call about itself.

The planner rejects any other value. A typo here is uniquely bad —
`on_error: contninue` reads as "keep going" to whoever wrote it and silently
means "stop" to the runner, and you would only find out on the night
something failed and the run went down anyway.

## What `continue` actually does

The bot fails, the run carries on, and **everything downstream of it is
skipped**. Continuing past a bot does not mean pretending it produced
output: without the skip, one tolerated failure cascades into a run of
"upstream bot has no recorded outputs yet" from every bot behind it.
Skipping is transitive, and a bot that never needed the failed one still
runs — this is about the data, not about proximity in the DAG.

`continue` is per bot, not a mood the whole run catches. A fatal failure
alongside a tolerated one still fails the run.

## It cannot be invisible

A run that finished with a hole in it is not a plain success, and the whole
point of continuing is that someone still finds out. So the failure is
recorded on the run as a *tolerated failure* and shown everywhere a result
is:

```
run 30adb321: succeeded
  continued past a failure in notifier: … Secret slack/bot_token not found …
```

and in the WebUI as **"Finished, but one step didn't run"** — in warning
colours, naming the bot, with the same one-click fix a real failure gets
("Connect Slack").

### Why not a third run status

`succeeded_with_errors` was the obvious alternative, and it would have to be
understood by the run store, the scheduler, every status filter and every
tone map — for a run whose honest one-word summary is still "finished". The
information matters more than the enum: a tolerated failure is persisted
with the run, survives a restart, and renders as a warning wherever a plain
success renders green. If that ever proves too quiet in practice, the status
is the next thing to add, not the first.

## Setting it

In the YAML, or in the builder: select a bot and the inspector's **If this
bot fails** picks between *Stop the whole run* and *Carry on without it*.
Going back to the default writes no `on_error` key at all, rather than
littering every swarm file with `on_error: stop`.

The AI composer sets it too — a trailing notification in a composed swarm
comes back marked `continue` (`docs/fan-out.md` covers the rest of what it
now writes).

## Retrying a bot

```yaml
bots:
  - id: watch
    use: competitor-watch@0.1.0
    retry: 2        # 0 (default) never retries; 3 is the ceiling
```

For a transient failure — a flaky service call, a container that lost its
network for a second — where pressing Run again is all a human would do.

**A retry re-runs the entire bot**, including anything it already did. A bot
that sent an email and then failed on its last step sends that email again.
So a retry on a bot declaring `guardrails.writes_allowed` is refused at plan
time rather than warned about at 3am:

```
FAIL bot "sender" has retry: 2, but it writes to gmail — a retry re-runs the
whole bot, so a failure after the write sends it again. Remove the retry, or
split the write into its own bot that isn't retried.
```

**A declined approval is never retried.** Asking again until someone says
yes is not a retry; it is wearing them down. Nor is a run that ended
underneath the bot — there is nothing left to run into.

Every retry is logged. A bot quietly succeeding on its third attempt every
night is worth knowing about the service behind it.

### Waiting between retries

```yaml
bots:
  - id: watch
    use: competitor-watch@0.1.0
    retry: 2
    retry_backoff: 5s   # empty (default) retries immediately; 60s is the ceiling
```

Empty by default, which retries immediately — most of this catalog's
transient failures are a container race, not a rate limit, and an immediate
retry is what a human hitting Run again would do anyway. Set it for a
service that actually wants space between attempts.

Refused at plan time, same as an absurd retry count: `retry_backoff` with no
`retry` has nothing to wait between, anything that isn't a Go duration
string (`5s`, `1m`) is refused rather than silently ignored, and anything
over 60s is refused too — past that, the honest answer is `on_error:
continue` plus a notification, not a longer sleep. A run stopped mid-wait
does not sit out the rest of it.

## Falling back to another bot

```yaml
bots:
  - id: fetch
    use: live-price-lookup@0.1.0
    retry: 2
    fallback: fixture-price-lookup@0.1.0
```

`retry` rides out a blip; `fallback` is for when the service is actually
down and the honest move is to degrade rather than stop. Once retries are
exhausted, the fallback bot runs in the failed one's place, its outputs land
under the same bot id, and downstream snaps never know the difference. The
run says so either way:

```
run 30adb321: succeeded
  falling back to fixture-price-lookup@0.1.0 after: connection reset
  fetch recovered using fallback fixture-price-lookup@0.1.0
```

**The fallback bot has to be a real substitute, not just a bot that happens
to exist.** `nanobots plan` refuses one whose output ports don't match the
primary's exactly, name for name and type for type — a downstream snap
type-checked against the primary bot's ports, and a fallback that changed
the shape would make that type-check a lie. It also refuses a fallback that
requires an input port the primary bot instance doesn't already have: the
fallback runs with this instance's own resolved inputs, nothing is re-wired
for it, so a port that was never there can never arrive.

Not itself retried, and not chained to a second fallback — one substitute is
the whole feature. A fallback that also fails ends the run exactly as if
there had been no fallback, naming both bots that were tried. And it never
runs in place of a declined approval or a stopped run: neither is a failure
a substitute bot can fix, and starting a whole new container to replace one
the user just told to stop would be exactly the kind of thing this repo
calls dishonest.

## When to use it

Use `continue` for work at the edge of a swarm that nothing else reads —
notifications, summaries, logging a result somewhere secondary.

Do not use it to paper over a bot that keeps failing. A swarm where the
tolerated failure happens every single night is a swarm quietly doing less
than it says, and the banner is there to make that obvious rather than
comfortable.

## Adopted from n8n, not copied

The idea is n8n's per-node "continue on fail". Only the idea: n8n is
fair-code under the Sustainable Use License rather than OSI open source, so
nothing here is derived from its source.

Two things are deliberately different. n8n's continue-on-fail passes an
error object *down* the graph as if it were data, so downstream nodes run
with a failure in hand; here the downstream bots are skipped, because a
nanobot's inputs are typed ports and there is no honest way to type "this is
an error". And the failure is surfaced on the run rather than left to be
noticed in an execution log.

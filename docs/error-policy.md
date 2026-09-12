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
runs — this is about the data, not about the wave.

`continue` is per bot, not a mood the wave catches. A fatal failure
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

# Lab: talk to your Team

`context/TEAM-LAB-DESIGN.md` proposed `Human -> Lab Agent -> Team agents ->
nanobots and nanoswarms`. `docs/team.md` built and proved the bottom layer.
This page is the layer above it: one chat tab, backed by an orchestrator
that decides whether to delegate your message to a Team member or just
answer it — never a third option that acts directly, since that would be
exactly the elevated-trust shortcut the design doc rules out.

## What it is

A tab (Advanced mode, next to Team) with one ongoing conversation. Type
what you want; Lab decides what to do with it:

- **Delegate** it to a Team role — an existing one, or a new one it names
  — and stream that role's real work back into the same chat as it
  happens.
- **Report status** on a role's recent work, read from its workspace's own
  commit history.
- **Just answer**, when there's nothing to delegate — a greeting, a
  clarifying question, ordinary conversation.

## Why it's a single-shot router, not a chat loop

`internal/llm.Generator` is one prompt in, one completion out — the same
constraint the AI composer (`internal/api/compose.go`) already lives with,
and there is no function-calling client in this build to change that. So
Lab asks for exactly one structured decision per message, restating the
whole conversation so far each time (Generate has no memory of its own
between calls — exactly what the composer's own prompts already have to
do, restating the entire bot catalog on every call). One decision, acted
on deterministically in Go, is what the composer already does successfully
for a harder version of the same problem — drafting a whole swarm — so Lab
does the smaller version of the same thing.

## Verified for real, not just against a mock

Sending an actual message through the real WebUI tab, against a real
1Claw Shroud call and a real Gemini Team engine, found two real bugs that
a fake generator in a unit test never would have:

**1. The delegated task never started an agent — every message failed
with "context canceled."** `handleLabMessage` passed the HTTP request's
own context into the background goroutine that runs the delegation; `net/
http` cancels a request's context the instant its handler returns, and
this handler returns almost immediately (202, then the client watches
SSE). So the model call was canceled before it could ever answer, every
single time. Fixed by using `context.Background()` for the goroutine —
see `internal/api/lab.go`.

**2. The final summary was a truncated fragment.** Gemini streams one
answer as several delta chunks, each its own `"text"` event (see
`docs/team.md`'s own transcript). The first version of the code that folds
a delegation's result back into the conversation kept only the *last*
chunk it saw rather than accumulating them, so Lab said things like
`designer: or created.` instead of the whole answer. Fixed in
`internal/lab.consumeDelegation`, and the accumulation logic was pulled out
into its own function specifically so this could be covered by a real
test afterward (`internal/lab/consume_test.go`, built from the actual
event sequence that exposed it) rather than trusting that watching it once
in a browser was enough.

A real, complete exchange, after both fixes:

```
You: Ask the designer to read docs/anatomy.md and tell us in one sentence
     what a nanobot is. Don't edit any files.

Lab: delegating to designer: Read docs/anatomy.md and tell us in one
     sentence what a nanobot is. Do not edit any files.

team/designer  tool  read_file docs/anatomy.md
team/designer  tool  Read lines 1-100 of 156 from docs/anatomy.md
team/designer  text  Based on `docs/anatomy.md`, a nanobot is a YAML-defined agent
                     that declares typed input and output ports and executes a
                     fixed list of steps to perform tasks without directly
                     exposing external credentials or API keys.
team/designer  result done

Lab: designer: Based on `docs/anatomy.md`, a nanobot is a YAML-defined
     agent that declares typed input and output ports and executes a fixed
     list of steps to perform tasks without directly exposing external
     credentials or API keys.
```

**One known cosmetic rough edge, left as such rather than "fixed" past what
was actually verified:** raw delta chunks are concatenated with no added
separator, and a chunk boundary doesn't always land on a word boundary —
one real run read "externalcredentials" where two chunks split mid-word.
Recorded here rather than patched with an unverified heuristic (inserting
a space at every chunk boundary would be wrong just as often, since some
chunks legitimately continue the previous word or punctuation).

## How it's wired

- `internal/lab.Session` holds the one ongoing conversation. It embeds a
  `*runner.Run` purely for its existing `Log`/`Subscribe`/`Unsubscribe`
  machinery — the same pub-sub a swarm run or a foundry job already uses
  for its own SSE stream (`internal/api/sse.go`, `foundry.go`), reused
  here rather than built a third time. Nothing about a chat session being
  called a "run" is meaningful, and no swarm executes because one exists.
- `POST /api/lab/messages` kicks off `Session.HandleMessage` in the
  background and returns immediately; `GET /api/lab/events` streams the
  conversation over SSE, replaying everything said so far to a tab opened
  mid-conversation, then staying subscribed — the identical shape
  `GET /api/runs/{id}/events` already has.
- There is exactly one session per server process, matching this build's
  single-tenant shape everywhere else. No persistence across a restart
  yet, and no multi-session story.

## What's deliberately not built yet

- **Status only covers a Team role, not a swarm.** The design doc named
  two things Lab should be able to read: what a Team agent is doing, and
  what a swarm is doing. Only the first is wired up (a role's git log);
  asking Lab about a swarm's run history today gets routed to "answer,"
  and it can only speak from what's in the conversation, not a real read
  of `internal/runner.RunStore`.
- **No conversation persistence.** A daemon restart loses the transcript —
  `internal/lab.Session` lives in memory only, the same as every other
  in-process run this build tracks before `runner.RunStore` picks it up
  for history; Lab's own conversation has no such store yet.
- **No cross-agent memory.** `context/TEAM-LAB-DESIGN.md`'s
  recall-before-work / remember-after-work pattern (Honcho, already
  shipped for bots) isn't wired into Team agents yet — see `docs/team.md`.
- **No multi-tenancy.** One Lab, one team, one nanobots install.

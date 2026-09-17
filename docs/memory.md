# Memory

What a bot remembers between runs.

Until now there was one option and it wasn't really memory: a flat key/value store on a 1Claw agent, reachable only if 1Claw was configured. Two bots used it, for `last_run_at` and `last_summary`. Worse, without a 1Claw key those bots quietly behaved differently — `competitor-watch` reported everything as new on every run, because of a credential that has nothing to do with what it was remembering.

## Two capabilities, not one

`internal/memory` has two interfaces on purpose, because they are genuinely different things and one interface would force every backend to fake whichever half it lacks:

| | what it does | who has it |
|---|---|---|
| `Store` | durable key/value, namespaced per bot | every backend |
| `Recaller` | accumulate observations, then ask questions in plain language | only backends that derive something |

A backend advertises `Recaller` by implementing it. Callers discover that with a type assertion and get a clear error otherwise — **not** an empty answer, because a bot behaving as though it remembered nothing is a silent wrong answer, and `memory.recall` is used to decide things.

`Composite` lets the halves come from different places, which is the arrangement most people want: key/value on local disk, recall somewhere that can reason over it.

## Backends

```
NANOBOTS_MEMORY      local (default) | 1claw | honcho
HONCHO_URL           e.g. http://localhost:8000
HONCHO_WORKSPACE     defaults to "nanobots"
HONCHO_API_KEY       omit for a self-hosted server (AUTH_USE_AUTH=false by default)
```

**`local`** — one JSON file per namespace under `~/.nanobots/memory/`, written temp-then-rename so a torn write can't lose every key in a namespace. It's the default so memory works out of the box. A namespace is a bot-supplied string, so an unsafe one (`../../escaped`) is hashed rather than joined, and a test asserts nothing lands outside the directory.

**`1claw`** — the previous behaviour, now one option among several. Key/value only: 1Claw's memory API derives nothing, so it does not implement `Recaller` and `memory.recall` against it fails with a clear message rather than returning nothing. Entries hang off an agent id, which only exists once a run reaches that bot, so startup leaves a `DeferredOneClaw` marker that the runner swaps for a real store per bot. Within an agent, the namespace is the bot's own name, so two bots sharing an agent do not share memories.

**One thing to know if you used this backend before September 2026.** Agents used to be one per bot and are now one per guardrail profile ([oneclaw-bridge.md](oneclaw-bridge.md)), so a bot's entries were written against `nanobots-inbox-triage` and are now read from `nanobots-redact-…`. Nothing is deleted, and nothing migrates: the old entries stay where they are and the bot starts with an empty memory. 1Claw has no API to move memory between agents, so the honest options are to let it relearn or to copy the values across by hand before pruning the old agents.

**`honcho`** — [Honcho](https://github.com/plastic-labs/honcho) is Workspace → Peer → Session, with a background deriver building a representation you can question. It is deliberately **not** a `Store`: it has no key/value semantics, and bolting `Get`/`Put` onto messages-and-representations would misrepresent it. Selecting it composes local key/value with Honcho recall.

## Steps

```yaml
- name: recall_prior
  type: memory.get          # one stored value, by key
  key: last_summary

- name: remember
  type: memory.put
  key: last_summary
  value: "{{steps.summarise.output}}"

- name: what_matters
  type: memory.recall       # a question, in plain language
  query: "what does this person usually escalate first?"

- name: note
  type: memory.remember     # an observation, for the backend to derive from
  value: "they escalated a refund again"
```

`memory.recall` takes `optional: true` when the answer improves a bot rather than being required. That distinction exists because the *default* backend is key/value and cannot answer questions — without it, any bot using recall would fail outright on a fresh install, which would make the feature unusable. The bot declares which it is rather than the engine guessing.

Getting that across the container boundary needed care. A bot runs in a container and reaches memory through a callback, so an `ErrNoRecall` returned as an error becomes a string and can no longer be type-asserted. The callback therefore reports it as data — `{"answer": "", "supported": false}` — and `RemoteDeps` rebuilds the sentinel, so the interpreter can still tell "no recall here" from "recall broke".

A recall step's output is self-describing or absent: `internal/step.optionalTextBlock` wraps a non-empty answer in a labelled `<remembered>` block and renders nothing at all otherwise, so a prompt never carries its own "what this person usually does:" header pointing at emptiness.

`memory.get`, `memory.put` and `memory.remember` are best-effort: a backend that can't do them logs and continues, because bookkeeping should not fail a run. **`memory.recall` is not.** A bot asking a question uses the answer to decide something, so a backend that can't answer errors rather than substituting silence. A recall-capable backend returning an empty answer is fine — that genuinely is "nothing known yet".

In demo mode `memory.recall` reads `fixtures/memory.recall.json` — a plain string, or `{"<question>": "<answer>"}` — so a recall-using bot stays conformance-testable offline like everything else.

A bot with **no** such fixture is treated as having no recall available, not as having an empty answer. That holds demo mode to the same contract as runtime: a bot whose recall step is required fails conformance without a fixture, exactly as it would fail at runtime on a key/value backend, while one marked `optional: true` degrades and still conforms. Verified by removing `optional: true` from `inbox-triage` and watching conformance fail, then putting it back.

## Verified

- **Bots really using it.** Three, each with a matching `memory.remember` so a recall-capable backend has something to derive from: `inbox-triage` recalls what this person has treated as urgent before, `support-triage` what this team has escalated to a human, and `draft-replies` what was already promised or declined to a correspondent. All three are `optional: true`. `support-triage` also ships a `fixtures/memory.recall.json`, so the catalog's conformance run exercises both the answered path and the degraded one rather than only the degraded one.

  `inbox-triage` recalls what this person has treated as urgent before (`optional: true`) and remembers each call. On the default key/value backend it logged `memory.recall skipped: this deployment has key/value memory only` and succeeded. Pointed at a recall-capable backend, the same bot and the same swarm — no code change, only `NANOBOTS_MEMORY=honcho` — logged `memory.recall ... -> 74 chars`, hitting `/v3/workspaces/nanobots/peers/inbox-triage/chat` and `/v3/workspaces/nanobots/sessions/inbox-triage-runs/messages`.
- **Local**, end to end and twice: `competitor-watch` logged `memory.get last_summary (found=false)` on its first run and `found=true` on its second, with the summary on disk between them — and no 1Claw involved in either.
- **Honcho**, against a fake server asserting the exact routes and bodies read out of its source: `POST /v3/workspaces/{ws}/sessions/{ns}-runs/messages` with `MessageBatchCreate` (`peer_name` is aliased `peer_id` on the wire), and `POST /v3/workspaces/{ws}/peers/{ns}/chat` with `DialecticOptions{query}` returning `DialecticResponse{content}`. Nullable content is treated as "nothing known", not an error, and no `Authorization` header is sent when no key is configured.

**Verified against a real Honcho server**, now that the repo ships one — see *Running Honcho locally* below. Every route and body shape the fake asserts was confirmed live: `POST .../messages` returned 201 with `peer_id` echoed back, and the dialectic answered a real question from three real observations. The one thing the fake got wrong was not a shape but a failure mode; see the same section.


## Running Honcho locally

```sh
./docker/honcho/honcho.sh up -d --build     # first run clones and builds; a few minutes
```

It needs one LLM provider key — Honcho uses a model to derive memories and to answer questions. The shipped config uses Gemini Flash Lite for everything and Gemini for embeddings, so a free key from https://aistudio.google.com/apikey is enough. Put it in `~/.secrets/nanobots.env` as `GEMINI_API_KEY`, alongside:

```
NANOBOTS_MEMORY=honcho
HONCHO_URL=http://localhost:8000
```

Then restart nanobotd. `/api/status` should say `local + honcho`, and Settings should stop warning you.

Honcho itself is not vendored — `honcho.sh` clones upstream on first run, so you get their Dockerfile and migrations rather than a copy that goes stale. `HONCHO_REF` pins what's checked out. Nothing here writes your key to disk: the script reads it from `~/.secrets/nanobots.env` and passes it to the containers as an environment variable.

### What running it for real changed

Two things a fake could not have told us.

**The free tier is small, and a dialectic query is not one request.** Gemini's free tier allows five `generate_content` calls a minute; a single dialectic query makes several, because the model calls `search_memory` and `search_messages` as tools before answering. Two bots in one swarm each asking one question exhausted the minute. `docker/honcho/config.toml` caps tool iterations per reasoning level for that reason — and because nanobots asks one short, specific question per run, which does not need ten rounds of tool use. Raise them on a paid key.

**`optional: true` was only half implemented.** It degraded when the backend *couldn't* answer questions, but not when the backend *failed* to. So the rate-limited second query took down a swarm that had already triaged the whole inbox. A bot that says it can work without recall must mean that for every reason there's no answer, not just one — fixed, and the reason is always logged so a run never gets quietly worse. A *required* recall step still fails.

Also worth knowing: model names expire. `gemini-2.5-flash` returns 404 for keys created after its retirement, which is how the first attempt failed. The config pins a version deliberately rather than tracking a `-latest` alias, so it will need bumping one day but won't change under you between runs.

## Seeing which backend you're on

`GET /api/status` reports `memory_backend` (`local`, `1claw`, `local + honcho`) and `memory_recall`, and Settings shows both.

That matters because the degradation is deliberately quiet. `inbox-triage` declares its recall step `optional: true`, so on a key/value backend it logs one line and carries on — triaging by its static rules instead of by what you've actually treated as urgent. The run succeeds either way, and the only difference is quality. A capability that changes how well the product works while leaving no trace in the UI is the same problem a stopped Docker daemon was, so Settings says so plainly and names the bots it currently affects.

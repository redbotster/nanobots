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

**`1claw`** — the previous behaviour, now one option among several. Key/value only: 1Claw's memory API derives nothing, so it does not implement `Recaller` and `memory.recall` against it fails with a clear message rather than returning nothing. Its namespace is the agent id, which only exists once a run reaches that bot, so startup leaves a `DeferredOneClaw` marker that the runner swaps for a real store per bot.

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

- **A bot really using it.** `inbox-triage` now recalls what this person has treated as urgent before (`optional: true`) and remembers each call. On the default key/value backend it logged `memory.recall skipped: this deployment has key/value memory only` and succeeded. Pointed at a recall-capable backend, the same bot and the same swarm — no code change, only `NANOBOTS_MEMORY=honcho` — logged `memory.recall ... -> 74 chars`, hitting `/v3/workspaces/nanobots/peers/inbox-triage/chat` and `/v3/workspaces/nanobots/sessions/inbox-triage-runs/messages`.
- **Local**, end to end and twice: `competitor-watch` logged `memory.get last_summary (found=false)` on its first run and `found=true` on its second, with the summary on disk between them — and no 1Claw involved in either.
- **Honcho**, against a fake server asserting the exact routes and bodies read out of its source: `POST /v3/workspaces/{ws}/sessions/{ns}-runs/messages` with `MessageBatchCreate` (`peer_name` is aliased `peer_id` on the wire), and `POST /v3/workspaces/{ws}/peers/{ns}/chat` with `DialecticOptions{query}` returning `DialecticResponse{content}`. Nullable content is treated as "nothing known", not an error, and no `Authorization` header is sent when no key is configured.

**Not verified against a real Honcho server.** Nothing in this repo can reach one, so the client is built and tested against shapes read from Honcho's own routers and schemas — the same standard `internal/oneclaw` holds itself to — but the first run against a live deployment is still the first run.


## Seeing which backend you're on

`GET /api/status` reports `memory_backend` (`local`, `1claw`, `local + honcho`) and `memory_recall`, and Settings shows both.

That matters because the degradation is deliberately quiet. `inbox-triage` declares its recall step `optional: true`, so on a key/value backend it logs one line and carries on — triaging by its static rules instead of by what you've actually treated as urgent. The run succeeds either way, and the only difference is quality. A capability that changes how well the product works while leaving no trace in the UI is the same problem a stopped Docker daemon was, so Settings says so plainly and names the one bot it currently affects.

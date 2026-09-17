# Testing

Expensive verification is one consolidated pass, not a pass after every
piece. This is exactly what that pass covers and how to repeat it.

## 1. Everything cheap and automated

```sh
go build ./... && go vet ./... && go test ./... -race
cd web && npx tsc -b && npm run test
```

671 table-driven Go tests across every package
(`grep -rho '^func Test[A-Za-z0-9_]*' --include='*_test.go' . | sort -u | wc -l`,
so the number stays checkable), including:

- `internal/contract`'s `TestRunConformanceOnLaunchBots` — auto-discovers and
  conformance-tests all 39 bots under `bots/` against their own fixtures, no
  Docker and no network.
- `internal/planner`'s `TestPlanAllExampleSwarms` — auto-discovers and
  type-checks all 16 swarms under `examples/swarms/`.
- httptest-mocked 1Claw/Google/Slack/GitHub/Stripe/HubSpot/X/LinkedIn
  clients, built against each provider's real documented endpoint shapes
  (verified against `@1claw/openapi-spec` and each provider's own docs, not
  guessed).
- `internal/runner`'s run-history tests — a run really written to a temp dir,
  a second store really reading it back, plus the awkward cases: a corrupt
  file, an over-cap directory, and a run left mid-flight by a restart
  ([run-history.md](run-history.md)).

The TypeScript side is strict-mode compilation plus Vitest and Testing
Library component tests, including the composer's request/response types and
the `useUIMode` hook.

## Claims that check themselves

`internal/contract` is where this repo asserts things about itself, because
every claim in prose is a claim that rots. Today it checks:

| Test | What would otherwise drift |
|---|---|
| `TestTheClaimedTestCountIsAccurate` | the number on this page |
| `TestTheHeadlineCatalogCountsAreAccurate` | **39 bots** / **16 swarms** wherever they are stated |
| `TestEveryDocCountIsCurrent` | any `N bots`/`N swarms` in any prose page, README and CLAUDE.md included |
| `TestTheDocsNameEveryBot` / `…EverySwarm` | a bot or swarm nobody wrote a row for |
| `TestEveryDocIsReachableFromTheReadme` | a page written, linked from nowhere, and invisible |
| `TestEveryRelativeDocLinkResolves` | a link left pointing at a page that moved or was renamed |
| `TestTheDocsQuoteTheRealFiles` | the YAML quoted in [anatomy.md](anatomy.md) drifting from the real files |
| `TestTheClaimedCronSwarmCountIsAccurate` | "twelve of the sixteen carry a cron trigger", repeated in four files |
| `TestTheClaimedWideSwarmCountIsAccurate` | [parallelism.md](parallelism.md)'s table and the count above it |
| `TestNoBotDemoOutputUsesAnEmDash` | the catalog modelling the habit `tone` exists to remove |
| `TestEveryProsePromptStatesTheHouseVoice` | a new prose bot learning its voice from the prompt's own style |
| `TestGeneratedSchemasAreUpToDate` | `schemas/*.json` drifting from the Go types |
| `TestTheBotContractDocNamesEveryStepType` | a new step type the contract doc never mentions |

When one of these fails, fix the claim. Do not weaken the test.

## 2. Real Docker executions against the live 1Claw API

Needs `ONECLAW_API_KEY` and Docker running:

```sh
go run ./cmd/nanobots run -f examples/swarms/never-drop-a-thread.yaml
go run ./cmd/nanobots run -f examples/swarms/content-engine.yaml
```

Both have been run end to end: real openclaw and bare containers, a real
Shroud-generated draft, a real approval gate answered from the terminal,
and — for `content-engine` — two independent live `ai.generate` calls chained
across separate containers followed by a real publish. Both finished
`succeeded`.

## 3. A live browser pass

Unit tests pass on things that are visibly broken. A browser pass has caught
what green tests did not: a nested `<button>`, a token masked in one place
and printed in another, a panel squeezed into a header's flex slot, the bot
palette silently unable to scroll past its first screenful, and a
pre-existing `email-drive-file` timeout bug whose `max_runtime_secs: 60`
guardrail was too short for its own approval gate to ever be answered in
time.

The shape of the pass, driven ad hoc against a real `nanobotd` plus
`vite dev`:

1. Load the app, confirm basic mode's nav has no "Bot library" entry.
2. Type `Help me automate a daily email recap and list it by priority` into
   the composer box and click **Automate it**.
3. Poll for the builder to open with a validated draft — a real
   `POST /api/compose` round trip to Shroud. Confirmed: a four-bot draft
   (`inbox-triage` → `recap-emails-to-pdf` → `drive-save` → `notify`) came
   back named "Daily Priority Email Recap", already marked ready to run by
   the planner.
4. Flip the header toggle to Advanced, confirm "Bot library" and "Build
   manually" now appear.

Two things worth knowing when measuring in a browser: React batches, so
reading `el.style` right after dispatching an event shows the *old* value
(split the dispatch and the read), and Radix Tabs do not respond to a
synthetic `.click()`.

## Conformance on its own

```sh
nanobots conform bots          # every bot against its fixtures
nanobots conform bots/<id>     # one bot
nanobots plan                  # every swarm type-checks
```

Neither needs Docker, a network, or a credential — which is exactly why the
foundry can hand them to a sandboxed coding agent as its only success
criterion ([foundry.md](foundry.md)).

// Mirrors internal/api's JSON shapes and the relevant subset of
// internal/schema's Nanobot fields. Kept hand-written and small rather than
// generated — the API surface is stable and narrow enough that a generator
// would be more ceremony than it saves right now.

export interface Port {
  name: string;
  type: string;
  default?: string;
  required?: boolean;
  mime?: string;
  description?: string;
}

export interface Service {
  id: string;
  provider: string;
  scopes?: string[];
  required?: boolean;
  connection?: string; // "demo" | "oauth_1claw" | "oauth_native" | "browser" | "api_key_vault"
}

export interface Guardrails {
  pii?: string;
  injection_threshold?: number;
  max_runtime_secs?: number;
  network_egress?: string[];
  writes_allowed?: string[];
  daily_budget_usd?: number;
  approval_required_for?: string[];
}

export interface BotSummary {
  id: string;
  name: string;
  version: string;
  description: string;
  tags: string[];
  harness: string;
  services: Service[];
  inputs: Port[];
  outputs: Port[];
  guardrails: Guardrails;
}

export interface SnapCheck {
  From: string;
  To: string;
  /** What arrives at the target — after any join. */
  FromType?: string;
  ToType?: string;
  /** How a fanned-out list collapses on the way through, if it does:
   * lines | json | count | flatten | first. See docs/fan-out.md. */
  join?: string;
  /** What the source port produced, before the join. Only set when one
   * happened — without it a joined snap reads as though twenty values had
   * always been one. */
  raw_from_type?: string;
  OK: boolean;
  error?: string;
}

export interface BotInstance {
  instance_id: string;
  bot_id: string;
  name: string;
  version: string;
}

/** A required input port with nothing to fill it — no snap feeds it and no
 * literal value was given. The backend computes this specifically so the
 * builder can say "mailer needs a `to`" while you're still wiring, instead
 * of letting you save something that dies several containers in. */
export interface UnfedInput {
  bot: string;
  port: string;
  reason: string;
}

export interface PlanResult {
  swarm: string;
  order?: string[];
  bots: BotInstance[];
  error?: string;
  snaps: SnapCheck[];
  unfed?: UnfedInput[];
  /** Swarm-level mistakes that belong to neither a snap nor a port — an
   * unrecognised on_error, a port both snapped and given a literal. */
  invalid?: string[];
  ok: boolean;
}

export type RunStatus =
  "pending" | "running" | "awaiting_approval" | "awaiting_unlock" | "succeeded" | "failed";

export interface LogEntry {
  time: string;
  bot: string;
  step?: string;
  msg: string;
  /** Set only on the entry announcing a swarm Lab just composed and saved. */
  open_swarm_path?: string;
}

export interface PendingApproval {
  id: string;
  bot: string;
  step: string;
  summary: string;
  risk_tier: string;
  created: string; /** What this bot may touch outside the machine — its own
   * guardrails.writes_allowed, so the prompt can show the blast radius of
   * "yes" instead of only the subject line. */
  writes?: string[];
}

/** The facts every view of a run needs, wherever it came from.
 *
 * Extracted because Run and RunSummary each declared their own copy of
 * these eight fields, and they drifted the moment one of them gained
 * `stopped_by_user`: the Runs list knew a run had been stopped and the run
 * detail page did not. One declaration, two shapes that extend it. */
export interface RunCore {
  id: string;
  swarm_name: string;
  status: RunStatus;
  started_at: string;
  finished_at?: string;
  error?: string;
  /** "manual" (a human clicked Run) or "schedule" (internal/scheduler
   * fired it) — see docs/scheduler.md. Lets the Runs page explain a run
   * nobody remembers starting. */
  triggered_by: "manual" | "schedule" | "webhook";
  /** The swarm file this was planned from — present on any real swarm run,
   * absent on a foundry job (which embeds a Run but has no swarm file).
   * It's what makes "Run it again" possible from the run itself. */
  swarm_path?: string;
  /** Set when someone stopped this run on purpose. The status is still
   * "failed" — it did not finish — but a run you ended yourself should not
   * look like one that broke. */
  stopped_by_user?: boolean;
  /** Set when this run ended because a human answered "no" to an approval.
   * Distinct from `stopped_by_user`: that is ending a run, this is
   * answering it. The status is still "failed" — the run did not finish —
   * but declining is a decision, and rendering it in red under
   * `not approved (decided_by=you)` tells someone their own "no" was a
   * fault. */
  declined_by_user?: boolean;
  /** Why this run did no work: a watch looked and found nothing new, so
   * every bot either stopped or was skipped behind one. The status is
   * still "succeeded", because that is what happened — but an hourly watch
   * writes twenty-four of those a day, and a list that calls them all
   * "succeeded" hides the one that acted. */
  nothing_to_do?: string;
  /** How many bots this run finished without (runner.ToleratedFailure) —
   * a count, not the failures themselves; the full list with each error
   * and remedy is what GET /api/runs/{id}'s `tolerated` already carries,
   * and RunDetail's own banner already renders it. The status is still
   * "succeeded", correctly — the run did finish — but a list that shows
   * every one of those in plain green looks identical to one where
   * nothing was skipped. */
  tolerated_count?: number;
}

/** What GET /api/runs returns per run: everything a list renders, and
 * nothing it doesn't. The full Run (with log and outputs) comes from
 * GET /api/runs/{id} — see internal/api/runs.go's runSummaryToJSON for why
 * they're different shapes. */
export interface RunSummary extends RunCore {
  /** A count, not the approvals themselves — enough for a badge and for
   * "N runs are waiting on you", which is all a list needs. */
  pending_approval_count: number;
}

export interface Run extends RunCore {
  log: LogEntry[];
  pending_approvals: PendingApproval[] | null;
  outputs: Record<string, Record<string, unknown>>;
  tolerated?: ToleratedFailure[];
  /** What to do about `error`, when the server recognises the failure. */
  remedy?: RunRemedy | null;
  /** "<bot>.<service>" pairs this run reached through fixtures rather than
   * a real account. A run made of demo data succeeds and looks exactly like
   * a real one — and that is the default, since every bot ships on
   * `connection: demo`. */
  demo_services?: string[];
}

/** A file-typed output's shape on the wire — see internal/runner's
 * collectOutputs, which stores it as a plain {uri, mime} map rather than a
 * step.FileValue struct specifically so it serializes cleanly here. */
export interface FileOutput {
  uri: string;
  mime: string;
}

export function isFileOutput(v: unknown): v is FileOutput {
  return (
    typeof v === "object" &&
    v !== null &&
    typeof (v as FileOutput).uri === "string" &&
    (v as FileOutput).uri.startsWith("nbf://")
  );
}

export interface StatusResponse {
  oneclaw_configured: boolean;
  /** Every bot runs in a container, so this being false means nothing can
   * run at all — reported separately from oneclaw because the two fail
   * independently and have unrelated fixes. */
  docker_available: boolean;
  /** One line, only when docker_available is false: "Docker isn't running",
   * "Docker isn't installed", or a timeout. */
  docker_reason: string;
  /** 1Claw's vault re-locks on its own schedule and needs passkey
   * verification. While locked, every bot with a Slack/GitHub/Stripe/HubSpot
   * credential fails — several steps into a run, after earlier bots have
   * already done real work. Reported up front for the same reason Docker is. */
  vault_locked: boolean;
  vault_reason: string;
  /** Which backend is behind memory.* steps: "local", "1claw",
   * "local + honcho". See docs/memory.md. */
  memory_backend: string;
  /** Whether that backend can answer questions, not just store keys. Bots
   * that could learn from history degrade quietly without it, so the
   * difference is worth showing. */
  memory_recall: boolean;
  /** The bots that actually ask memory a question, from the catalog — so
   * Settings can say what a key/value backend costs *this* install without
   * a bot name being hardcoded here and going stale. */
  memory_recall_bots: string[];
  /** Which LLM an ai.generate step reaches: "1claw shroud (token billing)",
   * "gemini (direct)", "openai-compatible (http://…)", or "none". */
  llm_backend: string;
  /** Whether prompts pass through 1Claw on the way out — per-agent budget,
   * PII redaction, injection screening. A direct provider key has none of
   * that, and nothing in a run looks different, so it's said here. */
  llm_guardrails: boolean;
  /** Where a pasted Slack/GitHub/Stripe/HubSpot token actually lands: a
   * sentence naming the backend and what it protects — "1Claw vault —
   * encrypted, access-logged, ...", "a local file, encrypted at rest —
   * ...", or "your OS keychain — ...". See docs/secrets.md. */
  secrets_backend: string;
}

export interface SwarmSummary {
  path: string;
  name: string;
  description: string;
  services_live: number;
  services_total: number;
  /** Absent when this swarm has never run this session — the gallery and
   * run history used to be two disconnected parts of the UI. */
  last_run_id?: string;
  last_run_status?: RunStatus;
  last_run_at?: string;
  last_run_trigger?: "manual" | "schedule" | "webhook";
  /** How many bots the last run finished without (runner.ToleratedFailure).
   * last_run_status is still "succeeded", correctly — but a card reading
   * "last ran 2h ago" in plain green looked identical whether every step
   * ran or one silently didn't. */
  last_run_tolerated?: number;
  /** "Weekdays at 7:00 AM" — the swarm's cron trigger in words. Absent when
   * the swarm has no cron trigger. See internal/scheduler/describe.go. */
  schedule?: string;
  /** The raw cron expression behind `schedule`, for anyone who wants it. */
  schedule_expr?: string;
  timezone?: string;
  next_run_at?: string;
  /** Set when the cron expression can't be parsed — such a swarm never
   * fires, and used to say so only in a daemon log line nobody reads. */
  schedule_error?: string;
  /** One of this swarm's bots stops to ask a human before it acts. Only
   * really interesting next to a schedule — see the card's schedule line. */
  needs_approval?: boolean;
  /** "cron" | "event" | "webhook" | "manual" — the declared trigger. */
  trigger_type?: string;
  /** The event this swarm declares but that nothing in this build fires,
   * so it runs only when someone clicks Run — which looked identical to a
   * swarm with no trigger at all. Cron has always fired and webhooks now do
   * too, so `event:` is the last one left. */
  inert_trigger?: string;
  /** True when this swarm's schedule has stopped firing because it kept
   * failing. See internal/scheduler/breaker.go — a swarm whose Slack was
   * never connected used to fail every 30 minutes forever. */
  schedule_paused?: boolean;
  /** Consecutive failed runs. Reported below the pause threshold too, so a
   * card can warn on the way down rather than only once it has stopped. */
  failure_streak?: number;
  /** The most recent failure's message — a streak is nearly always the
   * same fact repeated, so one line covers it. */
  streak_error?: string;
}

/** Where to post to fire a webhook swarm, and what to send with it. Fetched
 * on demand rather than carried on SwarmSummary — it contains a credential
 * and the swarm list is polled continuously. See handleWebhookDetails. */
export interface WebhookDetails {
  swarm: string;
  url: string;
  token: string;
  /** The whole thing as one runnable line, token included. */
  curl: string;
}

/** One bot instance's `loop:` bound — internal/schema.Loop, and
 * internal/api/builder.go's builderBotRef.Loop on the wire. No builder UI
 * sets this yet; see DraftBot.loop. */
export interface DraftLoop {
  max: number;
  feed?: Record<string, string>;
  until?: string;
}

/** One bot instance in a swarm draft, as the visual builder edits it — the
 * wire shape internal/api/builder.go's builderBotRef expects.
 *
 * Every field below but id/use/inputs is carried through the builder even
 * though nothing in the UI sets it yet: the builder round-trips a whole
 * swarm on every save, so a field it doesn't know about is a field it
 * deletes. retry_backoff, when, fallback, loop and swarm have no editor
 * yet — see docs/builder.md. */
export interface DraftBot {
  id: string;
  use: string; // "<bot-dir-id>@<version>" — empty when `swarm` is set instead
  inputs?: Record<string, unknown>;
  on_error?: string;
  retry?: number;
  retry_backoff?: string;
  when?: string;
  fallback?: string;
  loop?: DraftLoop;
  /** A path to another swarm's YAML, nested as this bot instance — an
   * alternative to `use`, never both. See schema.BotRef.Swarm. */
  swarm?: string;
  execution?: string;
}

/** A bot that failed while its swarm was told to carry on without it
 * (`on_error: continue`). A run carrying one of these finished, but with a
 * hole in it — see docs/error-policy.md. */
export interface ToleratedFailure {
  bot: string;
  error: string;
  remedy?: RunRemedy | null;
}

/** What to do about a failure, computed by the server.
 *
 * This list used to live in web/src/lib/runError.ts, which meant `nanobots
 * run` in a terminal printed a bare error for every one of them — a locked
 * vault, a missing model, an unconnected account — while the browser showed
 * the fix. It comes from internal/remedy now, so both callers read one
 * table. */
export interface RunRemedy {
  /** One sentence: what to do about it. */
  advice: string;
  /** A Settings-level fix, when there is one. The CLI ignores it: "click
   * Settings" means nothing in a terminal. */
  action?: { label: string; page: "settings" };
  /** Where to read more, when the fix isn't a button. */
  docs?: string;
}

/** One connection in a swarm draft — internal/api/builder.go's builderSnap. */
export interface DraftSnap {
  from: string;
  to: string;
  /** Carried through the builder even though nothing in the UI sets it
   * yet: the builder round-trips a swarm on every save, so a field it
   * doesn't know about is a field it deletes. */
  join?: string;
}

export interface ValidateSwarmRequest {
  bots: DraftBot[];
  snaps: DraftSnap[];
}

export interface SaveSwarmRequest {
  path?: string; // set to overwrite an existing swarm; omit to create a new one
  name: string;
  description?: string;
  bots: DraftBot[];
  snaps: DraftSnap[];
  /** A five-field cron expression, or "" for manual.
   *
   * Tri-state on purpose, matching the server: `undefined` means "leave
   * whatever this swarm already has alone", `""` means make it manual.
   * Sending "" on every save would quietly unschedule any swarm you edited.
   * See saveSwarmRequest in internal/api/builder.go. */
  schedule?: string;
  timezone?: string;
}

export interface SaveSwarmResult {
  path: string;
  name: string;
  description: string;
  plan: PlanResult;
}

/** A saved swarm's full structured bots/snaps — what the builder loads to
 * edit an existing swarm (as opposed to PlanResult's type-checked-but-lossy
 * summary, or the raw YAML text the read-only drawer shows). */
export interface SwarmFull {
  path: string;
  name: string;
  description: string;
  owner: string;
  bots: DraftBot[];
  snaps: DraftSnap[];
}

export type ConnectableService =
  "google" | "slack" | "github" | "stripe" | "hubspot" | "x" | "linkedin";

export interface ConnectionStatus {
  service: ConnectableService;
  connected: boolean;
}

/** When the composer determines no combination of the real catalog can
 * satisfy a request, it returns this instead of a draft — see
 * internal/api/compose.go's composeGapPayload. The WebUI offers to start a
 * foundry job (see FoundryJob) from here, but only if the human opts in. */
export interface ComposeGap {
  missing_capability: string;
  suggested_inputs?: Port[];
  suggested_outputs?: Port[];
}

/** The "head nanobot" composer's response: either a draft swarm assembled
 * from a plain-English request, already validated against the real
 * planner (never auto-saved; only ever handed to the builder for a human
 * to review), or a declared gap — never both. See internal/api/compose.go. */
export interface ComposeResult {
  draft?: SaveSwarmRequest;
  plan?: PlanResult;
  gap?: ComposeGap;
}

/** The subset of a run/job's shape RunLog actually reads — widened from
 * Run so the same log+approval UI renders a foundry job identically to a
 * swarm run, without RunLog needing to know which one it's showing. */
export interface LoggableJob {
  id: string;
  log: LogEntry[];
  pending_approvals: PendingApproval[] | null;
}

/** One foundry job (internal/foundry.Job, wire shape from
 * internal/api/foundry.go's foundryJobToJSON) — the escalation path behind
 * a composer gap. `bot` only appears once conformance passes, just before
 * the human review gate opens. */
export interface FoundryJob extends LoggableJob {
  status: RunStatus;
  started_at: string;
  finished_at?: string;
  error?: string;
  request: string;
  missing_capability: string;
  bot_id?: string;
  iterations: number;
  conform_ok: boolean;
  outcome?: "promoted" | "rejected" | "conform_failed" | "sandbox_violation" | "timeout" | "";
  bot?: BotSummary;
}

/** Which catalog bots use one provider, split by whether they're on demo
 * fixtures or the connected account — so the UI can offer "switch all 24"
 * with a real number instead of a vague promise. */
export interface ProviderBots {
  provider: string;
  demo: string[];
  live: string[];
}

export interface SetProviderConnectionResult {
  provider: string;
  changed: string[];
  /** bot id -> why it couldn't be switched. A partial failure is reported,
   * not swallowed: the bots that did switch really did. */
  failed?: Record<string, string>;
}

/** One bot whose behaviour you've changed, and where it works. The Bot
 * library is the catalog — every bot that exists. The Team is a different
 * question: who works for you, and how have you told them to behave. */
export interface TeamMember {
  bot_id: string;
  name: string;
  instructions: string;
  /** What it shipped with, so the UI can show the change and offer to put
   * it back. Meaningless unless instructions_tuned is true. */
  shipped: string;
  instructions_tuned: boolean;
  /** Whether this bot has an instructions port at all — false for a bot
   * present here only for a guardrails tune. */
  has_instructions: boolean;
  /** What applies now. */
  guardrails: Guardrails;
  /** What it shipped with, so the UI can offer to put it back.
   * Meaningless unless guardrails_tuned is true — it's the zero value,
   * not "shipped with nothing set", since no bot in this catalog actually
   * ships with every guardrail unset. */
  shipped_guardrails: Guardrails;
  guardrails_tuned: boolean;
  used_in: string[];
}

export interface Team {
  members: TeamMember[];
}

/** One perspective a review team can be built from.
 *
 * Not a bot: `reviewer` becomes a security engineer or a designer depending
 * on what it is handed, which is why a review team is a swarm shape rather
 * than eight near-identical bots (docs/supervisors.md). */
export interface Role {
  id: string;
  name: string;
  /** The one line the reviewer is given about its own perspective. */
  focus: string;
  /** What roles/roles.yaml says, when you have changed it. Absent when
   * unedited, or for a role you added — which has nothing to go back to. */
  shipped?: string;
  /** A role you wrote, rather than one that shipped. */
  custom?: boolean;
  /** Switched off: out of the roster, still in the library so it can be
   * switched back on. */
  retired?: boolean;
  /** Every review team must include this one. The board still picks the
   * rest — this is a floor, not a fixed team. */
  always?: boolean;
}

export interface RoleLibrary {
  roles: Role[];
  /** Exactly what a review board is shown. Returned by the server rather
   * than rebuilt here, so the page can show the real thing. */
  roster: string;
  error?: string;
}

/** One fixture a finished run would write into a bot's test data.
 *
 * Fixtures here are hand-written, which makes them a guess at what a model
 * or an API returns — and guesses drift. This is the real thing, offered
 * back. See docs/fixtures.md. */
export interface FixturePreview {
  file: string;
  /** "new" or "replaces" — overwriting committed test data is the part
   * worth looking at twice. */
  status: string;
  content: string;
  current?: string;
}

export interface RunFixtures {
  bots: Record<string, FixturePreview[]>;
  error?: string;
}

/** What a bundle turns out to contain, and — once confirmed — where it
 * landed. The preview call reports everything but `imported`/`path`. */
export interface ImportResult {
  name: string;
  requires?: string[];
  connects?: string[];
  /** What this swarm can write to when it runs. The one thing to read
   * before running a stranger's automation, and the reason import is two
   * steps rather than one. */
  acts?: string[];
  /** Catalog bots this machine doesn't have. Non-empty means nothing was
   * written, confirmed or not. */
  missing?: string[];
  imported: boolean;
  path?: string;
}

/** What the model calls have cost. Only 1Claw can answer this — on a direct
 * provider key `metered` is false and the spend is between the user and
 * that provider. See internal/api/spend.go. */
export interface SpendResponse {
  metered: boolean;
  /** Distinguishes "$0.00 so far" from "nothing metered yet". */
  known: boolean;
  spent_usd: number;
  period_from?: string;
  credit_usd?: number;
  credit_used_usd?: number;
  /** 1Claw's own words — a budget nearly spent, a balance running out. */
  warning?: string;
}

/** What 1Claw's OpenTelemetry surface says about this account. Shown as one
 * row in Settings rather than a page of its own — see internal/api/posture.go. */
export interface PostureResponse {
  configured: boolean;
  score: number;
  threats: number;
  critical: number;
  pending: number;
  agents: number;
  agent_limit?: number;
  /** How many of those agents this repo provisioned. Every distinct bot
   * name gets one, so this is usually most of them. */
  nanobots_agents: number;
  /** At 80% of the plan's agent allowance. Hitting the cap is a real
   * failure mode: EnsureAgent returns 403 mid-run. */
  agents_near_cap: boolean;
  tier?: string;
  error?: string;
}

/** Which coding-agent CLI a Lab delegation actually runs — see
 * internal/team.Engine. "" (only ever DefaultEngine) means neither
 * ANTHROPIC_API_KEY nor GEMINI_API_KEY is configured at all. */
export type TeamEngine = "claude" | "gemini" | "";

export interface LabEngineRole {
  role: string;
  /** Effective engine: the role's own override, or the default. */
  engine: TeamEngine;
  overridden: boolean;
}

export interface LabEnginesResponse {
  default_engine: TeamEngine;
  claude_configured: boolean;
  gemini_configured: boolean;
  /** Every role that has ever been delegated to — there's no fixed
   * catalog, a role exists exactly when internal/team has given it a
   * workspace once. */
  roles: LabEngineRole[];
}

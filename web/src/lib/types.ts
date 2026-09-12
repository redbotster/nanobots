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
  FromType?: string;
  ToType?: string;
  OK: boolean;
  error?: string;
}

export interface BotInstance {
  instance_id: string;
  bot_id: string;
  name: string;
  version: string;
}

export interface PlanResult {
  swarm: string;
  order?: string[];
  bots: BotInstance[];
  error?: string;
  snaps: SnapCheck[];
  ok: boolean;
}

export type RunStatus =
  | "pending"
  | "running"
  | "awaiting_approval"
  | "succeeded"
  | "failed";

export interface LogEntry {
  time: string;
  bot: string;
  step?: string;
  msg: string;
}

export interface PendingApproval {
  id: string;
  bot: string;
  step: string;
  summary: string;
  risk_tier: string;
  created: string;
}

/** What GET /api/runs returns per run: everything a list renders, and
 * nothing it doesn't. The full Run (with log and outputs) comes from
 * GET /api/runs/{id} — see internal/api/runs.go's runSummaryToJSON for why
 * they're different shapes. */
export interface RunSummary {
  id: string;
  swarm_name: string;
  status: RunStatus;
  started_at: string;
  finished_at?: string;
  error?: string;
  triggered_by: "manual" | "schedule";
  swarm_path?: string;
  pending_approval_count: number;
}

export interface Run {
  id: string;
  swarm_name: string;
  status: RunStatus;
  started_at: string;
  finished_at?: string;
  error?: string;
  log: LogEntry[];
  pending_approvals: PendingApproval[] | null;
  outputs: Record<string, Record<string, unknown>>;
  /** "manual" (a human clicked Run) or "schedule" (internal/scheduler
   * fired it) — see docs/scheduler.md. Lets the Runs page explain a run
   * nobody remembers starting. */
  triggered_by: "manual" | "schedule";
  /** The swarm file this was planned from — present on any real swarm run,
   * absent on a foundry job (which embeds a Run but has no swarm file).
   * It's what makes "Run it again" possible from the run itself. */
  swarm_path?: string;
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
  last_run_trigger?: "manual" | "schedule";
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
  /** "cron" | "event" | "webhook" | "manual" — the declared trigger. */
  trigger_type?: string;
  /** The event/webhook this swarm declares but that nothing in this build
   * fires. Only cron is wired up (internal/scheduler), so such a swarm runs
   * only when someone clicks Run — which looked identical to a swarm with
   * no trigger at all. */
  inert_trigger?: string;
}

/** One bot instance in a swarm draft, as the visual builder edits it — the
 * wire shape internal/api/builder.go's builderBotRef expects. */
export interface DraftBot {
  id: string;
  use: string; // "<bot-dir-id>@<version>"
  inputs?: Record<string, unknown>;
}

/** One connection in a swarm draft — internal/api/builder.go's builderSnap. */
export interface DraftSnap {
  from: string;
  to: string;
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

export type ConnectableService = "google" | "slack" | "github" | "stripe" | "hubspot" | "x" | "linkedin";

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
 * library is the catalog — every bot that exists. The Fleet is a different
 * question: who works for you, and how have you told them to behave. */
export interface FleetMember {
  bot_id: string;
  name: string;
  instructions: string;
  /** What it shipped with, so the UI can show the change and offer to put
   * it back. */
  shipped: string;
  used_in: string[];
}

export interface FleetTeam {
  swarm: string;
  path: string;
  members: string[];
  /** The subset of members you've tuned. */
  tuned: string[];
}

export interface Fleet {
  members: FleetMember[];
  teams: FleetTeam[];
}

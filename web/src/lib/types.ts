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
}

export interface SwarmSummary {
  path: string;
  name: string;
  description: string;
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

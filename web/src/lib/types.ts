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
}

export interface StatusResponse {
  oneclaw_configured: boolean;
}

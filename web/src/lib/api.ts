import type {
  Team,
  BotSummary,
  ComposeResult,
  ConnectableService,
  ConnectionStatus,
  FoundryJob,
  Port,
  PlanResult,
  ProviderBots,
  Run,
  RunSummary,
  SetProviderConnectionResult,
  SaveSwarmRequest,
  SaveSwarmResult,
  StatusResponse,
  SwarmFull,
  SwarmSummary,
  ValidateSwarmRequest,
  RoleLibrary,
  RunFixtures,
  WebhookDetails,
  ImportResult,
  SpendResponse,
  PostureResponse,
} from "./types";

async function reqText(path: string): Promise<string> {
  const res = await fetch(path);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.text();
}

/** Unchanged is what a conditional GET returns when the server says 304.
 * Distinct from `undefined` so a caller can tell "nothing changed" from
 * "there is no data". */
export const Unchanged = Symbol("unchanged");

/** A GET that skips the work when nothing has changed.
 *
 * The runs list is 91KB and polled every two seconds; almost every poll is
 * byte-identical to the last. With an ETag the server answers 304 with no
 * body, and — the part that matters more than bandwidth — the caller can
 * skip parsing it and skip the React state update, so an idle Runs page
 * stops re-rendering itself twice a second.
 *
 * The caller owns the tag rather than the browser: see writeJSONCached for
 * why the responses are no-store.
 */
export async function getIfChanged<T>(
  path: string,
  etag: string | null,
): Promise<{ data: T; etag: string | null } | typeof Unchanged> {
  const res = await fetch(path, {
    headers: etag ? { "If-None-Match": etag } : undefined,
  });
  if (res.status === 304) return Unchanged;
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return { data: (await res.json()) as T, etag: res.headers.get("ETag") };
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status} ${res.statusText}: ${body}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const api = {
  status: () => req<StatusResponse>("/api/status"),
  setupOneClawKey: (apiKey: string) =>
    req<{ ok: boolean; restart_required: boolean }>("/api/setup/oneclaw-key", {
      method: "POST",
      body: JSON.stringify({ api_key: apiKey }),
    }),
  listBots: () => req<BotSummary[]>("/api/bots"),
  listSwarms: () => req<SwarmSummary[]>("/api/swarms"),
  plan: (path: string) => req<PlanResult>(`/api/swarms/plan?path=${encodeURIComponent(path)}`),
  swarmYAML: (path: string) => reqText(`/api/swarms/yaml?path=${encodeURIComponent(path)}`),
  swarmFull: (path: string) => req<SwarmFull>(`/api/swarms/full?path=${encodeURIComponent(path)}`),
  describeSchedule: (expr: string) =>
    req<{ ok: boolean; human?: string; error?: string; next_run_at?: string }>(
      `/api/schedule/describe?expr=${encodeURIComponent(expr)}`,
    ),
  webhookDetails: (name: string) =>
    req<WebhookDetails>(`/api/swarms/${encodeURIComponent(name)}/webhook`),
  resumeSchedule: (name: string) =>
    req<{ ok: boolean }>(`/api/swarms/${encodeURIComponent(name)}/schedule/resume`, {
      method: "POST",
    }),
  importSwarm: (bundle: string, confirm: boolean) =>
    req<ImportResult>("/api/swarms/import", {
      method: "POST",
      body: JSON.stringify({ bundle, confirm }),
    }),
  validateSwarm: (draft: ValidateSwarmRequest) =>
    req<PlanResult>("/api/swarms/validate", {
      method: "POST",
      body: JSON.stringify(draft),
    }),
  saveSwarm: (draft: SaveSwarmRequest) =>
    req<SaveSwarmResult>("/api/swarms", {
      method: "POST",
      body: JSON.stringify(draft),
    }),
  compose: (message: string) =>
    req<ComposeResult>("/api/compose", {
      method: "POST",
      body: JSON.stringify({ message }),
    }),
  spend: () => req<SpendResponse>("/api/spend"),
  posture: () => req<PostureResponse>("/api/posture"),
  cancelRun: (id: string) =>
    req<{ ok: boolean }>(`/api/runs/${encodeURIComponent(id)}/cancel`, { method: "POST" }),
  listConnections: () => req<ConnectionStatus[]>("/api/connections"),
  connectToken: (service: ConnectableService, token: string) =>
    req<ConnectionStatus>(`/api/connections/${service}`, {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  connectGoogleStart: () =>
    req<ConnectionStatus>("/api/connections/google/start", { method: "POST" }),
  connectXStart: () => req<ConnectionStatus>("/api/connections/x/start", { method: "POST" }),
  connectLinkedInStart: () =>
    req<ConnectionStatus>("/api/connections/linkedin/start", { method: "POST" }),
  setBotServiceConnection: (botId: string, serviceId: string, live: boolean) =>
    req<BotSummary>(`/api/bots/${botId}/services/${serviceId}/connection`, {
      method: "POST",
      body: JSON.stringify({ live }),
    }),
  providerBots: (service: string) => req<ProviderBots>(`/api/connections/${service}/bots`),
  setProviderConnection: (service: string, live: boolean) =>
    req<SetProviderConnectionResult>(`/api/connections/${service}/bots`, {
      method: "POST",
      body: JSON.stringify({ live }),
    }),
  team: () => req<Team>("/api/team"),
  roles: () => req<RoleLibrary>("/api/roles"),
  runFixtures: (runId: string) => req<RunFixtures>(`/api/runs/${runId}/fixtures`),
  pinFixtures: (runId: string, body: { bot?: string; files?: string[] }) =>
    req<{ written: string[] }>(`/api/runs/${runId}/fixtures`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  setRole: (
    id: string,
    body: { name?: string; focus?: string; retired?: boolean; always?: boolean },
  ) => req<RoleLibrary>(`/api/roles/${id}`, { method: "POST", body: JSON.stringify(body) }),
  resetRole: (id: string) => req<RoleLibrary>(`/api/roles/${id}/reset`, { method: "POST" }),
  setBotInstructions: (botId: string, instructions: string) =>
    req<BotSummary>(`/api/bots/${botId}/instructions`, {
      method: "POST",
      body: JSON.stringify({ instructions }),
    }),
  startRun: (swarmPath: string) =>
    req<Run>("/api/runs", {
      method: "POST",
      body: JSON.stringify({ swarm_path: swarmPath }),
    }),
  getRun: (id: string) => req<Run>(`/api/runs/${id}`),
  listRuns: () => req<RunSummary[]>("/api/runs"),
  decideApproval: (runId: string, approvalId: string, approved: boolean) =>
    req<{ ok: boolean }>(`/api/runs/${runId}/approvals/${approvalId}/decide`, {
      method: "POST",
      body: JSON.stringify({ approved, decided_by: "you" }),
    }),
  sendLabMessage: (message: string) =>
    req<{ ok: boolean }>("/api/lab/messages", {
      method: "POST",
      body: JSON.stringify({ message }),
    }),
  startFoundryJob: (
    request: string,
    missingCapability: string,
    suggestedInputs?: Port[],
    suggestedOutputs?: Port[],
  ) =>
    req<FoundryJob>("/api/foundry", {
      method: "POST",
      body: JSON.stringify({
        request,
        missing_capability: missingCapability,
        suggested_inputs: suggestedInputs,
        suggested_outputs: suggestedOutputs,
      }),
    }),
  getFoundryJob: (id: string) => req<FoundryJob>(`/api/foundry/${id}`),
  listFoundryJobs: () => req<FoundryJob[]>("/api/foundry"),
  decideFoundryReview: (jobId: string, approvalId: string, approved: boolean) =>
    req<{ ok: boolean }>(`/api/foundry/${jobId}/approvals/${approvalId}/decide`, {
      method: "POST",
      body: JSON.stringify({ approved, decided_by: "you" }),
    }),
  blobUrl: (uri: string, mime?: string) => {
    const hash = uri.replace("nbf://sha256/", "");
    const q = mime ? `?mime=${encodeURIComponent(mime)}` : "";
    return `/api/blobs/${hash}${q}`;
  },
};

/** Subscribes to a run's SSE log stream, replaying history first (the
 * backend does the same on connect) then live entries as they arrive. */
export function subscribeRunEvents(
  runId: string,
  onEntry: (entry: import("./types").LogEntry) => void,
): () => void {
  const source = new EventSource(`/api/runs/${runId}/events`);
  source.onmessage = (ev) => {
    try {
      onEntry(JSON.parse(ev.data));
    } catch {
      // ignore malformed frames rather than tearing down the stream
    }
  };
  return () => source.close();
}

/** Mirrors subscribeRunEvents exactly, for a foundry job's log. */
export function subscribeFoundryEvents(
  jobId: string,
  onEntry: (entry: import("./types").LogEntry) => void,
): () => void {
  const source = new EventSource(`/api/foundry/${jobId}/events`);
  source.onmessage = (ev) => {
    try {
      onEntry(JSON.parse(ev.data));
    } catch {
      // ignore malformed frames rather than tearing down the stream
    }
  };
  return () => source.close();
}

/** Mirrors subscribeRunEvents exactly, for Lab's one ongoing
 * conversation — no id, since there is exactly one session per server. */
export function subscribeLabEvents(
  onEntry: (entry: import("./types").LogEntry) => void,
): () => void {
  const source = new EventSource("/api/lab/events");
  source.onmessage = (ev) => {
    try {
      onEntry(JSON.parse(ev.data));
    } catch {
      // ignore malformed frames rather than tearing down the stream
    }
  };
  return () => source.close();
}

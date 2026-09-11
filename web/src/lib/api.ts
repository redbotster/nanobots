import type {
  BotSummary,
  ComposeResult,
  ConnectableService,
  ConnectionStatus,
  PlanResult,
  Run,
  SaveSwarmRequest,
  SaveSwarmResult,
  StatusResponse,
  SwarmFull,
  SwarmSummary,
  ValidateSwarmRequest,
} from "./types";

async function reqText(path: string): Promise<string> {
  const res = await fetch(path);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.text();
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
  listBots: () => req<BotSummary[]>("/api/bots"),
  listSwarms: () => req<SwarmSummary[]>("/api/swarms"),
  plan: (path: string) =>
    req<PlanResult>(`/api/swarms/plan?path=${encodeURIComponent(path)}`),
  swarmYAML: (path: string) =>
    reqText(`/api/swarms/yaml?path=${encodeURIComponent(path)}`),
  swarmFull: (path: string) =>
    req<SwarmFull>(`/api/swarms/full?path=${encodeURIComponent(path)}`),
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
  listConnections: () => req<ConnectionStatus[]>("/api/connections"),
  connectToken: (service: ConnectableService, token: string) =>
    req<ConnectionStatus>(`/api/connections/${service}`, {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  connectGoogleStart: () =>
    req<ConnectionStatus>("/api/connections/google/start", { method: "POST" }),
  startRun: (swarmPath: string) =>
    req<Run>("/api/runs", {
      method: "POST",
      body: JSON.stringify({ swarm_path: swarmPath }),
    }),
  getRun: (id: string) => req<Run>(`/api/runs/${id}`),
  listRuns: () => req<Run[]>("/api/runs"),
  decideApproval: (runId: string, approvalId: string, approved: boolean) =>
    req<{ ok: boolean }>(`/api/runs/${runId}/approvals/${approvalId}/decide`, {
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

import type {
  BotSummary,
  PlanResult,
  Run,
  StatusResponse,
  SwarmSummary,
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

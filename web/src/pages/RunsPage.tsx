import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { Run } from "../lib/types";
import { StatusDot } from "../components/StatusDot";

const tone: Record<Run["status"], "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  failed: "danger",
  pending: "muted",
};

export function RunsPage() {
  const [runs, setRuns] = useState<Run[]>([]);

  useEffect(() => {
    const load = () => api.listRuns().then(setRuns).catch(() => {});
    load();
    const id = setInterval(load, 2000);
    return () => clearInterval(id);
  }, []);

  const sorted = [...runs].sort((a, b) => b.started_at.localeCompare(a.started_at));

  return (
    <div className="h-full overflow-auto p-6 sm:p-8">
      <h1 className="font-display text-xl font-medium text-ink">Runs</h1>
      <p className="mt-1 text-sm text-muted">
        Every time a swarm ran this session, local to this nanobotd — history
        doesn't persist across a restart yet.
      </p>

      {sorted.length === 0 && (
        <p className="mt-8 text-sm text-muted">
          Nothing has run yet. Open a swarm and hit Run once.
        </p>
      )}

      <div className="mt-6 flex flex-col gap-2">
        {sorted.map((run) => (
          <div
            key={run.id}
            className="fade-in flex items-center gap-3 rounded-lg border border-edge bg-panel px-4 py-3"
          >
            <StatusDot tone={tone[run.status]} />
            <div className="min-w-0 flex-1">
              <div className="font-display text-sm text-ink">{run.swarm_name}</div>
              <div className="text-[11px] text-muted">
                {new Date(run.started_at).toLocaleString()} · {run.id.slice(0, 8)}
              </div>
            </div>
            <div className="text-xs text-muted">{run.status.replace("_", " ")}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

import { useState } from "react";
import type { Run } from "../lib/types";
import { useRuns } from "../lib/runsFeed";
import { StatusDot } from "../components/StatusDot";
import { RunDetail } from "./RunDetail";
import { relativeTime } from "../lib/relativeTime";
import { shortRunError } from "../lib/runError";

const tone: Record<Run["status"], "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  failed: "danger",
  pending: "muted",
};

export function RunsPage({ onOpenSettings }: { onOpenSettings?: () => void }) {
  // null (not []) until the first fetch lands, so the empty state doesn't
  // flash "nothing has run yet" at someone who does in fact have runs.
  const runs = useRuns();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  if (selectedId) {
    return (
      <RunDetail
        onOpenSettings={onOpenSettings}
        runId={selectedId}
        onBack={() => setSelectedId(null)}
        onOpenRun={setSelectedId}
      />
    );
  }

  const sorted = [...(runs ?? [])].sort((a, b) => b.started_at.localeCompare(a.started_at));
  const needsApproval = sorted.filter((r) => r.status === "awaiting_approval");

  return (
    <div className="h-full overflow-auto p-5 sm:p-6">
      <h1 className="font-display text-xl font-medium text-ink">Runs</h1>
      <p className="mt-1 hidden text-sm text-muted sm:block">
        Every swarm run on this machine, kept across restarts. Click one to
        watch it live or see what it produced, even if it started somewhere
        else — or to read why it failed and run it again.
      </p>

      {needsApproval.length > 0 && (
        <div className="mt-4 rounded-lg border border-warn/40 bg-warn/[0.06] px-4 py-3 text-sm text-warn">
          {needsApproval.length === 1
            ? "1 run is waiting on your approval."
            : `${needsApproval.length} runs are waiting on your approval.`}
        </div>
      )}

      {runs === null && <p className="mt-6 text-sm text-muted/60">Loading runs…</p>}
      {runs !== null && sorted.length === 0 && (
        <p className="mt-6 text-sm text-muted">
          Nothing has run yet. Open a swarm and hit Run once.
        </p>
      )}

      <div className="mt-5 flex flex-col gap-1.5">
        {sorted.map((run) => (
          <button
            key={run.id}
            onClick={() => setSelectedId(run.id)}
            className="fade-in flex items-center gap-3 rounded-lg border border-edge bg-panel px-4 py-3 text-left transition-colors hover:border-tron"
          >
            <StatusDot tone={tone[run.status]} />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span className="font-display text-sm text-ink">{run.swarm_name}</span>
                {run.triggered_by === "schedule" && (
                  <span
                    className="rounded-full border border-edge px-1.5 py-0.5 text-[9px] text-muted"
                    title="Fired automatically by the scheduler, not a manual Run click"
                  >
                    ⏰ scheduled
                  </span>
                )}
              </div>
              <div className="text-[11px] text-muted" title={new Date(run.started_at).toLocaleString()}>
                {relativeTime(run.started_at)} · {run.id.slice(0, 8)}
              </div>
              {/* One line of the failure right here — enough to tell "Docker
                  isn't running" from "the bot crashed" without opening it.
                  Only failed rows pay the extra line. */}
              {run.status === "failed" && run.error && (
                <div className="truncate text-[11px] text-danger" title={run.error}>
                  {shortRunError(run.error)}
                </div>
              )}
            </div>
            <div className="text-xs text-muted">{run.status.replace("_", " ")}</div>
          </button>
        ))}
      </div>
    </div>
  );
}

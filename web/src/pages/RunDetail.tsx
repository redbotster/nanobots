import { useState } from "react";
import { useRun } from "../lib/useRun";
import { api } from "../lib/api";
import { Tabs } from "../components/Tabs";
import { RunLog } from "../components/RunLog";
import { RunResults } from "../components/RunResults";
import { StatusDot } from "../components/StatusDot";

const tone: Record<string, "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  failed: "danger",
  pending: "muted",
};

/** A run viewed on its own, independent of whichever SwarmView (if any)
 * originally started it — the same run.go/RunStore backs both, so an
 * approval sitting here is exactly as real, live, and answerable as the one
 * inline in the swarm canvas. This is what closes the gap where navigating
 * away from a running swarm used to mean losing all access to it. */
export function RunDetail({
  runId,
  onBack,
  onOpenRun,
}: {
  runId: string;
  onBack: () => void;
  /** Lets "Run it again" hand the caller the brand-new run's id so the page
   * follows the retry instead of stranding you on the corpse of the old one. */
  onOpenRun?: (runId: string) => void;
}) {
  const { run } = useRun(runId);
  const [rerunning, setRerunning] = useState(false);
  const [rerunError, setRerunError] = useState<string | null>(null);

  const finished = run?.status === "failed" || run?.status === "succeeded";
  const canRerun = finished && !!run?.swarm_path;

  const rerun = async () => {
    if (!run?.swarm_path) return;
    setRerunning(true);
    setRerunError(null);
    try {
      const next = await api.startRun(run.swarm_path);
      onOpenRun?.(next.id);
    } catch (e) {
      setRerunError(String(e));
    } finally {
      setRerunning(false);
    }
  };

  return (
    <div className="grid h-full grid-rows-[auto_1fr] p-5 sm:p-6">
      <div>
        <button
          onClick={onBack}
          className="mb-3 flex items-center gap-1 font-display text-xs text-muted hover:text-ink"
        >
          ← Runs
        </button>
        <div className="flex items-center gap-2">
          <h1 className="font-display text-xl font-medium text-ink">
            {run?.swarm_name ?? "run"}
          </h1>
          {run && (
            <span className="flex items-center gap-1.5 text-xs text-muted">
              <StatusDot tone={tone[run.status] ?? "muted"} />
              {run.status.replace("_", " ")}
            </span>
          )}
          {run?.triggered_by === "schedule" && (
            <span
              className="rounded-full border border-edge px-1.5 py-0.5 text-[9px] text-muted"
              title="Fired automatically by the scheduler, not a manual Run click"
            >
              ⏰ scheduled
            </span>
          )}
          {canRerun && (
            <button
              onClick={rerun}
              disabled={rerunning}
              className="ml-auto shrink-0 rounded border border-edge-strong px-2.5 py-1 font-display text-xs text-muted transition-colors hover:border-tron hover:text-ink disabled:opacity-50"
              title={`Plan and run ${run?.swarm_path} again, exactly as it ran here`}
            >
              {rerunning ? "Starting…" : "↻ Run it again"}
            </button>
          )}
        </div>
        <p className="mt-1 text-[11px] text-muted">{runId}</p>

        {/* The backend has always sent this; nothing rendered it, so a failed
            run said "failed" and made you read the whole log to find out why. */}
        {run?.status === "failed" && run.error && (
          <div className="mt-3 rounded-lg border border-danger/40 bg-danger/[0.06] px-3.5 py-2.5">
            <div className="font-display text-[11px] uppercase tracking-wider text-danger">
              Why it failed
            </div>
            <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap break-words text-[12px] leading-snug text-ink">
              {run.error}
            </pre>
          </div>
        )}
        {rerunError && (
          <div className="mt-3 rounded-lg border border-danger/40 bg-danger/[0.06] px-3.5 py-2.5 text-[12px] text-danger">
            Couldn't start it again: {rerunError}
          </div>
        )}
      </div>

      <div className="mt-4 min-h-0 rounded-lg border border-edge bg-panel/40">
        <Tabs
          defaultValue="log"
          tabs={[
            { value: "log", label: "Run log", content: <RunLog run={run} /> },
            { value: "results", label: "Results", content: <RunResults run={run} /> },
          ]}
        />
      </div>
    </div>
  );
}

import { useRun } from "../lib/useRun";
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
export function RunDetail({ runId, onBack }: { runId: string; onBack: () => void }) {
  const { run } = useRun(runId);

  return (
    <div className="grid h-full grid-rows-[auto_1fr] p-6 sm:p-8">
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
        </div>
        <p className="mt-1 text-[11px] text-muted">{runId}</p>
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

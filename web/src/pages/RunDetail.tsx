import { useState } from "react";
import { useRun } from "../lib/useRun";
import { api } from "../lib/api";
import { Tabs } from "../components/Tabs";
import { RunLog } from "../components/RunLog";
import { RunResults } from "../components/RunResults";
import { StatusDot } from "../components/StatusDot";
import { parseRunError, runRemedy } from "../lib/runError";
import type { ToleratedFailure } from "../lib/types";

const tone: Record<string, "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  failed: "danger",
  pending: "muted",
};

/** The reason first, the full wrapped error behind a disclosure. The chain
 * that produced an error (container -> agent -> bot -> step -> callback) is
 * worth keeping, but it isn't what you're looking for when a run went red. */
function FailureBanner({
  error,
  onOpenSettings,
}: {
  error: string;
  onOpenSettings?: () => void;
}) {
  const [showRaw, setShowRaw] = useState(false);
  const parts = parseRunError(error);
  const remedy = runRemedy(error);
  if (!parts) return null;
  const hasMore = parts.message.trim() !== error.trim();
  return (
    <div className="mt-3 rounded-lg border border-danger/40 bg-danger/[0.06] px-3.5 py-2.5">
      <div className="flex items-center gap-2">
        <span className="font-display text-[11px] uppercase tracking-wider text-danger">
          Why it failed
        </span>
        {parts.origin && (
          <span className="rounded border border-danger/30 px-1.5 py-0.5 text-[10px] text-danger/80">
            {parts.origin}
          </span>
        )}
      </div>
      <p className="mt-1 whitespace-pre-wrap break-words text-[12px] leading-snug text-ink">
        {parts.message}
      </p>
      {/* What to do about it. The reason alone leaves someone to work out
          that "Secret slack/bot_token not found" means "go to Settings and
          connect Slack" — which is one click away, and was six of the
          fifteen catalog swarms' only problem. */}
      {remedy && (
        <div className="mt-2 flex flex-wrap items-center gap-2 rounded border border-edge bg-void/60 px-2.5 py-1.5">
          <span className="text-[12px] leading-snug text-muted">{remedy.advice}</span>
          {remedy.action && onOpenSettings && (
            <button
              onClick={onOpenSettings}
              className="rounded border border-tron/50 px-2 py-0.5 text-[11px] text-tron transition-colors hover:bg-tron/10"
            >
              {remedy.action.label}
            </button>
          )}
          {remedy.docs && (
            <code className="text-[11px] text-muted/70">{remedy.docs}</code>
          )}
        </div>
      )}
      {hasMore && (
        <>
          <button
            onClick={() => setShowRaw((v) => !v)}
            className="mt-1.5 text-[11px] text-muted underline decoration-dotted hover:text-ink"
          >
            {showRaw ? "Hide full error" : "Show full error"}
          </button>
          {showRaw && (
            <pre className="mt-1.5 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded bg-void px-2 py-1.5 text-[11px] text-muted">
              {error}
            </pre>
          )}
        </>
      )}
    </div>
  );
}

/** Bots the swarm was told to continue past. The run did its real work —
 * get-paid's reminders went out — and something at the edge didn't. Both
 * halves of that sentence matter, so this says "finished" and then names
 * exactly what is missing. */
function ToleratedBanner({
  tolerated,
  onOpenSettings,
}: {
  tolerated: ToleratedFailure[];
  onOpenSettings?: () => void;
}) {
  return (
    <div className="mt-3 rounded-lg border border-warn/40 bg-warn/[0.06] px-3.5 py-2.5">
      <span className="font-display text-[11px] uppercase tracking-wider text-warn">
        Finished, but {tolerated.length === 1 ? "one step" : `${tolerated.length} steps`} didn't run
      </span>
      <p className="mt-1 text-[12px] leading-snug text-muted">
        This swarm is set to carry on without {tolerated.length === 1 ? "it" : "them"}, so the rest
        of the run completed.
      </p>
      {tolerated.map((t) => {
        const parts = parseRunError(t.error);
        const remedy = runRemedy(t.error);
        return (
          <div key={t.bot} className="mt-2 border-t border-warn/20 pt-2">
            <span className="rounded border border-warn/30 px-1.5 py-0.5 text-[10px] text-warn/80">
              {t.bot}
            </span>
            <p className="mt-1 whitespace-pre-wrap break-words text-[12px] leading-snug text-ink">
              {parts?.message ?? t.error}
            </p>
            {remedy && (
              <div className="mt-1.5 flex flex-wrap items-center gap-2">
                <span className="text-[12px] leading-snug text-muted">{remedy.advice}</span>
                {remedy.action && onOpenSettings && (
                  <button
                    onClick={onOpenSettings}
                    className="rounded border border-tron/50 px-2 py-0.5 text-[11px] text-tron transition-colors hover:bg-tron/10"
                  >
                    {remedy.action.label}
                  </button>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

/** A run viewed on its own, independent of whichever SwarmView (if any)
 * originally started it — the same run.go/RunStore backs both, so an
 * approval sitting here is exactly as real, live, and answerable as the one
 * inline in the swarm canvas. This is what closes the gap where navigating
 * away from a running swarm used to mean losing all access to it. */
export function RunDetail({
  runId,
  onBack,
  onOpenRun,
  onOpenSettings,
}: {
  runId: string;
  onBack: () => void;
  /** Lets "Run it again" hand the caller the brand-new run's id so the page
   * follows the retry instead of stranding you on the corpse of the old one. */
  onOpenRun?: (runId: string) => void;
  /** Lets a failure offer its own fix — see runRemedy. */
  onOpenSettings?: () => void;
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
          <FailureBanner error={run.error} onOpenSettings={onOpenSettings} />
        )}
        {/* A run that finished with a hole in it. Rendered as a warning
            rather than left to the log, because the point of continuing
            past a failure is that someone still finds out. */}
        {run?.status === "succeeded" && (run.tolerated?.length ?? 0) > 0 && (
          <ToleratedBanner tolerated={run.tolerated!} onOpenSettings={onOpenSettings} />
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

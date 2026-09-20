import { useEffect, useRef } from "react";
import { useFoundryJob } from "../lib/useFoundryJob";
import { api } from "../lib/api";
import { RunLog } from "../components/RunLog";
import { BotCard } from "../components/BotCard";
import { StatusDot } from "../components/StatusDot";
import { Button } from "../components/Button";

const STATUS_TONE: Record<string, "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  awaiting_unlock: "warn",
  failed: "danger",
  pending: "muted",
};

/** The foundry's job screen: a sandboxed coding agent authoring one new
 * bot, live — reuses RunLog wholesale for the streaming log and approval
 * row (a foundry job is a LoggableJob, same as a swarm run), then shows
 * the authored bot as a normal BotCard once conformance passes, with
 * Approve/Reject wired to the review gate. On promotion, automatically
 * re-submits the original request to the composer — the human never has
 * to re-type or re-click anything to get back to the normal build flow. */
export function FoundryJobPage({
  jobId,
  backLabel,
  onPromoted,
  onDone,
}: {
  jobId: string;
  /** What onDone actually returns to — "Swarms" when opened from the
   * composer's own gap escalation, "Runs" when reopened later from the
   * nav badge/Runs banner (see App.tsx). A fixed "← Swarms" was wrong for
   * the second case: it named a page onDone doesn't go back to. */
  backLabel: string;
  onPromoted: () => void;
  onDone: () => void;
}) {
  const { job } = useFoundryJob(jobId);
  const firedPromoted = useRef(false);

  useEffect(() => {
    if (job?.outcome === "promoted" && !firedPromoted.current) {
      firedPromoted.current = true;
      onPromoted();
    }
  }, [job?.outcome, onPromoted]);

  if (!job) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted">
        Starting the foundry…
      </div>
    );
  }

  const decide = (approvalId: string, approved: boolean) =>
    api.decideFoundryReview(job.id, approvalId, approved).then(() => {
      if (!approved) onDone();
    });

  return (
    <div className="grid h-full grid-rows-[auto_1fr] overflow-hidden">
      <header className="flex flex-wrap items-center gap-3 border-b border-edge px-5 py-3">
        <button onClick={onDone} className="text-sm text-muted hover:text-ink">
          ← {backLabel}
        </button>
        <div className="flex items-center gap-2 text-sm">
          <StatusDot tone={STATUS_TONE[job.status] ?? "muted"} />
          <span className="font-display font-semibold text-ink">Building a new bot</span>
          <span className="text-muted">— {job.missing_capability}</span>
        </div>
        {job.outcome && job.outcome !== "promoted" && (
          <span className="ml-auto text-[12px] text-danger">{job.outcome.replace("_", " ")}</span>
        )}
      </header>

      <div className="grid min-h-0 grid-rows-[1fr] overflow-hidden sm:grid-cols-[1fr_360px] sm:grid-rows-1">
        <RunLog run={job} onDecide={decide} />

        <div className="overflow-auto border-t border-edge p-4 sm:border-l sm:border-t-0">
          {job.bot ? (
            <>
              <p className="mb-3 text-[13px] text-muted">
                Conformance passed. Review it, then approve to add it to the catalog — or reject to
                discard it.
              </p>
              <BotCard bot={job.bot} />
            </>
          ) : (
            <p className="text-[13px] text-muted">
              Waiting for the agent to author and self-test a bot
              {job.iterations > 0 ? ` (attempt ${job.iterations})` : ""}…
            </p>
          )}
          {job.outcome === "rejected" && (
            <div className="mt-4 flex justify-end">
              <Button variant="ghost" onClick={onDone}>
                Back to {backLabel}
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

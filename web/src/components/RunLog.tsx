import { useEffect, useRef } from "react";
import type { LoggableJob } from "../lib/types";
import { api } from "../lib/api";
import { Button } from "./Button";

function timeOf(iso: string) {
  return new Date(iso).toLocaleTimeString([], { hour12: false });
}

/** Renders a run or a foundry job's live log identically — both are
 * LoggableJob-shaped (id/log/pending_approvals), so this never needs to
 * know which one it's showing. onDecide defaults to the swarm-run
 * approval endpoint; pass FoundryJobPage's own handler to route a
 * decision to /api/foundry/... instead. */
export function RunLog({
  run,
  onDecide,
}: {
  run: LoggableJob | null;
  onDecide?: (approvalId: string, approved: boolean) => void;
}) {
  const endRef = useRef<HTMLDivElement>(null);
  const decide = onDecide ?? ((approvalId: string, approved: boolean) => {
    if (run) api.decideApproval(run.id, approvalId, approved);
  });

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: "end" });
  }, [run?.log.length]);

  if (!run) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted">
        Nothing's run yet. Hit <span className="mx-1 text-ink">Run once</span>{" "}
        above to see it happen here, live.
      </div>
    );
  }

  return (
    <div className="grid h-full grid-rows-[1fr_auto] overflow-hidden">
      <div className="overflow-auto px-4 py-3 font-mono text-[12.5px] leading-relaxed">
        {run.log.map((entry, i) => (
          <div key={i} className="fade-in grid grid-cols-[64px_100px_1fr] gap-3 py-0.5">
            <span className="text-muted">{timeOf(entry.time)}</span>
            <span className="truncate text-tron">
              {entry.bot}
              {entry.step ? `/${entry.step}` : ""}
            </span>
            <span
              className={
                entry.msg.toLowerCase().startsWith("failed")
                  ? "text-danger"
                  : "text-ink"
              }
            >
              {entry.msg}
            </span>
          </div>
        ))}
        {run.log.length === 0 && (
          <div className="text-muted">waiting for the first step…</div>
        )}
        <div ref={endRef} />
      </div>

      {run.pending_approvals?.map((pa) => (
        <div
          key={pa.id}
          className="fade-in flex flex-col gap-2 border-t border-warn/40 bg-warn/[0.06] px-4 py-3 sm:flex-row sm:items-center"
        >
          <div className="text-sm">
            <span className="font-display font-semibold text-warn">
              Approval needed
            </span>
            <span className="ml-2 text-ink">{pa.summary}</span>
          </div>
          <div className="flex gap-2 sm:ml-auto">
            <Button
              variant="ghost"
              className="border-edge text-muted"
              onClick={() => decide(pa.id, false)}
            >
              Skip
            </Button>
            <Button variant="primary" onClick={() => decide(pa.id, true)}>
              Approve
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}

import { useEffect, useRef, useState } from "react";
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
  onDecide?: (approvalId: string, approved: boolean) => void | Promise<void>;
}) {
  const endRef = useRef<HTMLDivElement>(null);
  // This is the one control that authorises a real send, post or payment.
  // It used to be fire-and-forget — no await, no catch, no disabled state,
  // no confirmation — so a failed decide call was a completely silent
  // no-op: the button looked clicked, nothing happened, and the run just
  // sat there waiting.
  const [deciding, setDeciding] = useState<string | null>(null);
  const [decideError, setDecideError] = useState<string | null>(null);

  const decide = async (approvalId: string, approved: boolean) => {
    setDeciding(approvalId);
    setDecideError(null);
    try {
      if (onDecide) {
        await onDecide(approvalId, approved);
      } else if (run) {
        await api.decideApproval(run.id, approvalId, approved);
      }
    } catch (e) {
      setDecideError(String(e));
    } finally {
      setDeciding(null);
    }
  };

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: "end" });
  }, [run?.log.length]);

  if (!run) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted">
        Nothing's run yet. Hit <span className="mx-1 text-ink">Run once</span> above to see it
        happen here, live.
      </div>
    );
  }

  return (
    <div className="grid h-full grid-rows-[1fr_auto] overflow-hidden">
      <div className="overflow-auto px-4 py-3 font-mono text-[12.5px] leading-relaxed">
        {run.log.map((entry, i) => (
          // At 375px the fixed 64px + 100px columns took 60% of the width
          // and left the message about a hundred pixels to wrap in. The
          // timestamp is the least useful of the three on a phone — you are
          // watching a run happen, not auditing when — so it drops out and
          // the bot column narrows. Both come back at sm.
          <div
            key={i}
            className="fade-in grid grid-cols-[72px_1fr] gap-2 py-0.5 sm:grid-cols-[64px_100px_1fr] sm:gap-3"
          >
            <span className="hidden text-muted sm:inline" title={entry.time}>
              {timeOf(entry.time)}
            </span>
            <span
              className="truncate text-tron"
              title={`${entry.bot}${entry.step ? "/" + entry.step : ""} · ${timeOf(entry.time)}`}
            >
              {entry.bot}
              {entry.step ? `/${entry.step}` : ""}
            </span>
            <span
              className={
                entry.msg.toLowerCase().startsWith("failed")
                  ? "break-words text-danger"
                  : "break-words text-ink"
              }
            >
              {entry.msg}
            </span>
          </div>
        ))}
        {run.log.length === 0 && <div className="text-muted">waiting for the first step…</div>}
        <div ref={endRef} />
      </div>

      {run.pending_approvals?.map((pa) => (
        <div
          key={pa.id}
          className="fade-in flex flex-col gap-2 border-t border-warn/40 bg-warn/[0.06] px-4 py-3 sm:flex-row sm:items-center"
        >
          <div className="min-w-0 text-sm">
            <span className="font-display font-semibold text-warn">Approval needed</span>
            {pa.risk_tier && (
              <span className="ml-2 rounded border border-warn/40 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-warn">
                {pa.risk_tier} risk
              </span>
            )}
            <span className="ml-2 text-ink">{pa.summary}</span>
            {/* What "yes" actually permits. The prompt used to name the
                subject line and nothing else, which tells you what the
                thing is about and not what approving does. */}
            <div className="mt-0.5 text-[12px] text-muted">
              {pa.writes?.length
                ? `${pa.bot} will write to ${pa.writes.join(", ")}. Declining stops the run here.`
                : `${pa.bot} is waiting on you. Declining stops the run here.`}
            </div>
          </div>
          <div className="flex shrink-0 gap-2 sm:ml-auto">
            <Button
              variant="ghost"
              className="border-edge text-muted"
              disabled={deciding === pa.id}
              onClick={() => void decide(pa.id, false)}
            >
              {/* Not "Skip": this does not skip a step and carry on, it
                  ends the run. A label that reads like "later" for a
                  button that means "no" is the wrong kind of gentle. */}
              Don't approve
            </Button>
            <Button
              variant="primary"
              disabled={deciding === pa.id}
              onClick={() => void decide(pa.id, true)}
            >
              {deciding === pa.id ? "Sending…" : "Approve"}
            </Button>
          </div>
          {decideError && (
            <p className="mt-2 w-full text-[12px] text-danger">
              Couldn't record that decision: {decideError}
            </p>
          )}
        </div>
      ))}
    </div>
  );
}

import { useState } from "react";
import { useRun } from "../lib/useRun";
import { refreshRuns } from "../lib/runsFeed";
import { api } from "../lib/api";
import { Tabs } from "../components/Tabs";
import { RunLog } from "../components/RunLog";
import { RunResults } from "../components/RunResults";
import { StatusDot } from "../components/StatusDot";
import { parseRunError } from "../lib/runError";
import type { RunRemedy } from "../lib/types";
import type { ToleratedFailure } from "../lib/types";
import { PinFixtures } from "../components/PinFixtures";

const tone: Record<string, "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  awaiting_unlock: "warn",
  failed: "danger",
  pending: "muted",
};

/** The reason first, the full wrapped error behind a disclosure. The chain
 * that produced an error (container -> agent -> bot -> step -> callback) is
 * worth keeping, but it isn't what you're looking for when a run went red. */
function FailureBanner({
  error,
  // Computed by the server (internal/remedy) rather than here, so `nanobots
  // run` in a terminal gets the same advice from the same table. The browser
  // used to own this list, which is why the CLI printed a bare error for a
  // locked vault or a missing model and left you to work it out.
  remedy,
  onOpenSettings,
  // A run that ended because you answered "no" gets the same banner in a
  // neutral tone. The content is right either way — remedy.go's advice for
  // this case is literally "This wasn't a fault: the approval was
  // declined" — and putting that sentence in a red box headed "Why it
  // failed" contradicts itself on screen.
  decision = false,
}: {
  error: string;
  remedy?: RunRemedy | null;
  onOpenSettings?: () => void;
  decision?: boolean;
}) {
  const [showRaw, setShowRaw] = useState(false);
  const parts = parseRunError(error);
  if (!parts) return null;
  const hasMore = parts.message.trim() !== error.trim();
  return (
    <div
      className={
        decision
          ? "mt-3 rounded-lg border border-edge-strong bg-panel px-3.5 py-2.5"
          : "mt-3 rounded-lg border border-danger/40 bg-danger/[0.06] px-3.5 py-2.5"
      }
    >
      <div className="flex items-center gap-2">
        <span
          className={
            decision
              ? "font-display text-[11px] uppercase tracking-wider text-muted"
              : "font-display text-[11px] uppercase tracking-wider text-danger"
          }
        >
          {decision ? "Why it stopped" : "Why it failed"}
        </span>
        {parts.origin && (
          <span
            className={
              decision
                ? "rounded border border-edge px-1.5 py-0.5 text-[10px] text-muted"
                : "rounded border border-danger/30 px-1.5 py-0.5 text-[10px] text-danger/80"
            }
          >
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
          {remedy.docs && <code className="text-[11px] text-muted/70">{remedy.docs}</code>}
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

/** A run that reached some services through fixtures.
 *
 * The most misleading thing this product can do is succeed convincingly on
 * invented data. Every bot ships on `connection: demo`, so that is the
 * default experience — plausible emails, plausible invoices, a green run,
 * and nothing anywhere saying none of it was real. */
function DemoDataBanner({
  services,
  onOpenSettings,
}: {
  services: string[];
  onOpenSettings?: () => void;
}) {
  // Group by service, since "gmail" twice is one fact about one account.
  const names = Array.from(new Set(services.map((s) => s.split(".").slice(1).join("."))));
  return (
    <div className="mt-3 rounded-lg border border-edge-strong bg-panel/50 px-3.5 py-2.5">
      <span className="font-display text-[11px] uppercase tracking-wider text-muted">
        Demo data
      </span>
      <p className="mt-1 text-[12px] leading-snug text-ink">
        {names.join(", ")} {names.length === 1 ? "was" : "were"} answered from this repo's example
        data, not your account. Nothing here was read from or written to anything real.
      </p>
      {onOpenSettings && (
        <button
          onClick={onOpenSettings}
          className="mt-1.5 rounded border border-tron/50 px-2 py-0.5 text-[11px] text-tron transition-colors hover:bg-tron/10"
        >
          Connect an account
        </button>
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
        const remedy = t.remedy;
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
  const [stopping, setStopping] = useState(false);
  const [stopError, setStopError] = useState<string | null>(null);
  const stop = async () => {
    if (!run) return;
    setStopping(true);
    setStopError(null);
    try {
      await api.cancelRun(run.id);
      refreshRuns();
    } catch (e) {
      setStopError(e instanceof Error ? e.message : String(e));
    } finally {
      setStopping(false);
    }
  };
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
    // A scrolling column, not grid-rows-[auto_1fr].
    //
    // The banners above the tabs are all conditional — demo data, pinnable
    // fixtures, a tolerated failure, a rerun error — and a run can carry
    // several at once. In a grid whose first track is `auto` they take their
    // full content height, the 1fr tab row collapses to nothing, and with no
    // overflow anywhere the tab content is not merely below the fold but
    // unreachable: on a 720px viewport a succeeded run with three banners
    // left the Results panel 48 pixels tall and the page would not scroll.
    //
    // flex-1 keeps the tabs filling the space when there is some, min-h
    // keeps them usable when there is not, and overflow-auto means anything
    // that does not fit can still be scrolled to.
    <div className="flex h-full flex-col overflow-auto p-5 sm:p-6">
      <div>
        <button
          onClick={onBack}
          className="mb-3 flex items-center gap-1 font-display text-xs text-muted hover:text-ink"
        >
          ← Runs
        </button>
        <div className="flex items-center gap-2">
          <h1 className="font-display text-xl font-medium text-ink">{run?.swarm_name ?? "run"}</h1>
          {run && (
            <span className="flex items-center gap-1.5 text-xs text-muted">
              <StatusDot
                tone={
                  run.stopped_by_user || run.declined_by_user
                    ? "muted"
                    : (tone[run.status] ?? "muted")
                }
              />
              {run.stopped_by_user
                ? "stopped"
                : run.declined_by_user
                  ? "declined"
                  : run.status.replace("_", " ")}
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
          {run?.triggered_by === "webhook" && (
            <span
              className="rounded-full border border-edge px-1.5 py-0.5 text-[9px] text-muted"
              title="Started by something posting to this swarm's webhook, not a manual Run click"
            >
              ⚡ webhook
            </span>
          )}
          {!finished && run && (
            // A running swarm holds a container until it finishes or hits
            // its ceiling — up to thirty minutes. Watching was the only
            // option before this.
            <button
              onClick={() => void stop()}
              disabled={stopping}
              className="ml-auto shrink-0 rounded border border-edge-strong px-2.5 py-1 font-display text-xs text-muted transition-colors hover:border-danger hover:text-danger disabled:opacity-50"
              title="Stop this run and its container now"
            >
              {stopping ? "Stopping…" : "■ Stop"}
            </button>
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
        {stopError && <p className="mt-1 text-[12px] text-danger">Couldn't stop it: {stopError}</p>}

        {/* The backend has always sent this; nothing rendered it, so a failed
            run said "failed" and made you read the whole log to find out why. */}
        {run?.status === "failed" && run.error && !run.stopped_by_user && (
          <FailureBanner
            error={run.error}
            remedy={run.remedy}
            onOpenSettings={onOpenSettings}
            decision={run.declined_by_user}
          />
        )}
        {/* A run that finished with a hole in it. Rendered as a warning
            rather than left to the log, because the point of continuing
            past a failure is that someone still finds out. */}
        {(run?.demo_services?.length ?? 0) > 0 && (
          <DemoDataBanner services={run!.demo_services!} onOpenSettings={onOpenSettings} />
        )}
        {run?.status === "succeeded" && <PinFixtures runId={run.id} />}
        {run?.status === "succeeded" && (run.tolerated?.length ?? 0) > 0 && (
          <ToleratedBanner tolerated={run.tolerated!} onOpenSettings={onOpenSettings} />
        )}
        {rerunError && (
          <div className="mt-3 rounded-lg border border-danger/40 bg-danger/[0.06] px-3.5 py-2.5 text-[12px] text-danger">
            Couldn't start it again: {rerunError}
          </div>
        )}
      </div>

      <div className="mt-4 min-h-72 flex-1 rounded-lg border border-edge bg-panel/40">
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

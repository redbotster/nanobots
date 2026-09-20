import { useMemo, useState } from "react";
import type { FoundryJob, Run, RunSummary } from "../lib/types";
import { useRuns } from "../lib/runsFeed";
import { StatusDot } from "../components/StatusDot";
import { RunDetail } from "./RunDetail";
import { relativeTime } from "../lib/relativeTime";
import { shortRunError } from "../lib/runError";

const tone: Record<Run["status"], "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  awaiting_unlock: "warn",
  failed: "danger",
  pending: "muted",
};

type Filter = "all" | "needs-you" | "failed" | "succeeded";

/** A run, plus the runs immediately before it that failed the same way.
 *
 * A real machine's history: 198 runs, 135 failed, and 85 of those were one
 * swarm failing identically because Slack was never connected. Rendered one
 * row each, that is a wall of red that reads as "everything is broken" and
 * hides the two failures that were actually different. Rendered as one row
 * saying "and 42 more like this", it reads as what it is: one fact. */
type Group = { head: RunSummary; repeats: RunSummary[] };

/** Failures group only *consecutive* identical rows, so a different run in
 * the middle splits the group. The list is a story in order; collapsing a
 * failure across one would rewrite it — something else happened in between,
 * which is itself worth knowing before the next occurrence of the same
 * failure.
 *
 * A "found nothing" run carries no story of its own — it has one fact
 * ("polled, nothing changed") that is exactly as true regardless of what
 * any other swarm did in between. So it collapses across the *whole* list
 * per swarm, not just consecutively. That distinction is not cosmetic: a
 * real machine with two watch bots on interleaved hourly crons
 * (meeting-to-action, repurpose-everything) never produces two consecutive
 * quiet runs from the same swarm at all — every quiet run has the other
 * swarm's quiet run sitting right before it — so consecutive-only grouping
 * collapsed nothing, and the wall of alternating "nothing to do" rows this
 * feature exists to prevent came right back. */
function groupRuns(sorted: RunSummary[]): Group[] {
  const key = (r: RunSummary) => {
    if (r.status === "failed") return `failed ${r.swarm_name} ${shortRunError(r.error ?? "")}`;
    if (r.nothing_to_do) return `quiet ${r.swarm_name}`;
    return null;
  };

  const out: Group[] = [];
  const quietGroupByKey = new Map<string, Group>();
  for (const run of sorted) {
    const k = key(run);
    if (run.nothing_to_do && k !== null) {
      const existing = quietGroupByKey.get(k);
      if (existing) {
        existing.repeats.push(run);
        continue;
      }
      const group: Group = { head: run, repeats: [] };
      out.push(group);
      quietGroupByKey.set(k, group);
      continue;
    }
    const last = out[out.length - 1];
    if (last && k !== null && key(last.head) === k) {
      last.repeats.push(run);
      continue;
    }
    out.push({ head: run, repeats: [] });
  }
  return out;
}

/** A run that went wrong, as opposed to one that did what you told it.
 *
 * Both of these end `failed`, because the run did not finish — but neither
 * is something to go debugging. Stopping a run is ending it; declining an
 * approval is answering it. The Failed count and the Failed filter share
 * this one predicate so the tab cannot say 94 and then list 92.
 */
function reallyFailed(r: RunSummary): boolean {
  return r.status === "failed" && !r.stopped_by_user && !r.declined_by_user;
}

export function RunsPage({
  onOpenSettings,
  pendingFoundryJobs = [],
  onOpenFoundryJob,
}: {
  onOpenSettings?: () => void;
  /** Foundry jobs awaiting a human review of the bot they authored — the
   * other half of what the nav badge counts (see useApprovalNotifications).
   * Rendered here, not just counted, so the badge's number and what
   * clicking it shows finally agree. */
  pendingFoundryJobs?: FoundryJob[];
  onOpenFoundryJob?: (jobId: string) => void;
}) {
  // null (not []) until the first fetch lands, so the empty state doesn't
  // flash "nothing has run yet" at someone who does in fact have runs.
  const runs = useRuns();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const sorted = useMemo(
    () => [...(runs ?? [])].sort((a, b) => b.started_at.localeCompare(a.started_at)),
    [runs],
  );
  const needsApproval = sorted.filter((r) => r.status === "awaiting_approval");

  const counts = {
    all: sorted.length,
    "needs-you": needsApproval.length,
    failed: sorted.filter(reallyFailed).length,
    succeeded: sorted.filter((r) => r.status === "succeeded").length,
  };

  const visible = sorted.filter((r) => {
    switch (filter) {
      case "needs-you":
        return r.status === "awaiting_approval";
      case "failed":
        return reallyFailed(r);
      case "succeeded":
        return r.status === "succeeded";
      default:
        return true;
    }
  });
  const groups = groupRuns(visible);

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

  return (
    <div className="h-full overflow-auto p-5 sm:p-6">
      <h1 className="font-display text-xl font-medium text-ink">Runs</h1>
      <p className="mt-1 hidden text-sm text-muted sm:block">
        Every swarm run on this machine, kept across restarts. Click one to watch it live or see
        what it produced, even if it started somewhere else — or to read why it failed and run it
        again.
      </p>

      {needsApproval.length > 0 && (
        // Clickable: a banner that tells you something is waiting and then
        // makes you go find it is doing half a job. One waiting run opens
        // straight into it; several filter the list down to them.
        <button
          onClick={() =>
            needsApproval.length === 1 ? setSelectedId(needsApproval[0].id) : setFilter("needs-you")
          }
          className="mt-4 w-full rounded-lg border border-warn/40 bg-warn/[0.06] px-4 py-3 text-left text-sm text-warn transition-colors hover:border-warn"
        >
          {needsApproval.length === 1
            ? "1 run is waiting on your approval — open it"
            : `${needsApproval.length} runs are waiting on your approval — show them`}
        </button>
      )}

      {/* Foundry jobs are the other half of what the nav badge counts, and
          used to have nowhere to land once you'd navigated away from the
          exact composer-gap screen that first opened one — the badge said
          N, this page (silently, since it has no idea foundry jobs exist)
          showed N-1. One row per job rather than folding into the swarm-run
          list above: a FoundryJob isn't a Run, and forcing it through
          needsApproval's filter/count machinery would just move the same
          "the count doesn't match what's shown" bug somewhere subtler. */}
      {pendingFoundryJobs.length > 0 && onOpenFoundryJob && (
        <div className="mt-2 flex flex-col gap-1.5">
          {pendingFoundryJobs.map((job) => (
            <button
              key={job.id}
              onClick={() => onOpenFoundryJob(job.id)}
              className="w-full rounded-lg border border-warn/40 bg-warn/[0.06] px-4 py-3 text-left text-sm text-warn transition-colors hover:border-warn"
            >
              <span className="font-display font-semibold">A new bot needs your review</span>
              <span className="mt-0.5 block truncate text-[12px] text-warn/80" title={job.request}>
                {job.request}
              </span>
            </button>
          ))}
        </div>
      )}

      {sorted.length > 0 && (
        <div className="mt-4 flex flex-wrap gap-1.5">
          {(
            [
              ["all", "All"],
              ["needs-you", "Needs you"],
              ["failed", "Failed"],
              ["succeeded", "Succeeded"],
            ] as [Filter, string][]
          ).map(([id, label]) => (
            <button
              key={id}
              onClick={() => setFilter(id)}
              disabled={counts[id] === 0 && id !== "all"}
              className={`rounded-full border px-2.5 py-1 text-[12px] transition-colors disabled:opacity-40 ${
                filter === id
                  ? "border-tron bg-tron/10 text-ink"
                  : "border-edge text-muted hover:border-tron hover:text-ink"
              }`}
            >
              {label}
              <span className="ml-1.5 text-muted/70">{counts[id]}</span>
            </button>
          ))}
        </div>
      )}

      {runs === null && <p className="mt-6 text-sm text-muted/60">Loading runs…</p>}
      {runs !== null && sorted.length === 0 && (
        <p className="mt-6 text-sm text-muted">
          Nothing has run yet. Open a swarm and hit Run once.
        </p>
      )}
      {runs !== null && sorted.length > 0 && visible.length === 0 && (
        <p className="mt-6 text-sm text-muted">Nothing matches that filter.</p>
      )}

      <div className="mt-4 flex flex-col gap-1">
        {groups.map(({ head, repeats }) => (
          <div key={head.id}>
            <RunRow run={head} onOpen={() => setSelectedId(head.id)} />
            {repeats.length > 0 && (
              <>
                <button
                  onClick={() => setExpanded((e) => ({ ...e, [head.id]: !e[head.id] }))}
                  className="mt-0.5 w-full rounded border border-dashed border-edge px-4 py-1 text-left text-[11px] text-muted transition-colors hover:border-tron hover:text-ink"
                >
                  {expanded[head.id]
                    ? "hide"
                    : head.nothing_to_do
                      ? `and ${repeats.length} more with nothing to do`
                      : `and ${repeats.length} more that failed the same way`}
                  <span className="ml-1.5 text-muted/60">
                    {relativeTime(repeats[repeats.length - 1].started_at)} –{" "}
                    {relativeTime(head.started_at)}
                  </span>
                </button>
                {expanded[head.id] && (
                  <div className="mt-0.5 flex flex-col gap-1 border-l border-edge pl-3">
                    {repeats.map((r) => (
                      <RunRow key={r.id} run={r} onOpen={() => setSelectedId(r.id)} />
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function RunRow({ run, onOpen }: { run: RunSummary; onOpen: () => void }) {
  return (
    <button
      onClick={onOpen}
      className="fade-in flex w-full items-center gap-3 rounded-lg border border-edge bg-panel px-4 py-2.5 text-left transition-colors hover:border-tron"
    >
      {/* Muted for a run that did nothing: it succeeded, and a green dot
          claiming work happened is the same small lie as calling a stopped
          run failed. Declining is the third of these — you answered the
          question, and a red dot says you broke something. A tolerated
          failure is different from all three: nothing here was decided or
          settled, a step just didn't run, so it gets warn (amber) rather
          than muted — the same "cannot be invisible" rule
          runner.ToleratedFailure's own doc comment states. */}
      <StatusDot
        tone={
          run.stopped_by_user || run.declined_by_user || run.nothing_to_do
            ? "muted"
            : run.tolerated_count
              ? "warn"
              : tone[run.status]
        }
      />
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
          <span
            className="text-[11px] text-muted"
            title={new Date(run.started_at).toLocaleString()}
          >
            {relativeTime(run.started_at)}
          </span>
        </div>
        {/* One line of the failure right here — enough to tell "Docker
            isn't running" from "the bot crashed" without opening it.
            Only failed rows pay the extra line. */}
        {run.status === "failed" && run.error && !run.stopped_by_user && !run.declined_by_user && (
          <div className="truncate text-[11px] text-danger" title={run.error}>
            {shortRunError(run.error)}
          </div>
        )}
        {/* Which gate, in muted text rather than red. Hiding it entirely
            (what a stopped run does) would be worse here: a schedule that
            keeps asking is worth seeing, and the error names the step. */}
        {run.declined_by_user && run.error && (
          <div className="truncate text-[11px] text-muted" title={run.error}>
            {shortRunError(run.error)}
          </div>
        )}
        {run.nothing_to_do && (
          <div className="truncate text-[11px] text-muted" title={run.nothing_to_do}>
            {run.nothing_to_do}
          </div>
        )}
        {/* A succeeded run that finished without one of its bots — see
            RunDetail's ToleratedBanner for the full "which bot, why, and
            what to do about it" (this is a list row, not the place for
            that much detail). Gated on the other three being absent so a
            row never carries two competing explanations at once. */}
        {!!run.tolerated_count &&
          !run.stopped_by_user &&
          !run.declined_by_user &&
          !run.nothing_to_do && (
            <div
              className="truncate text-[11px] text-warn"
              title={`${run.tolerated_count} bot${run.tolerated_count === 1 ? "" : "s"} finished without running — open the run for details`}
            >
              {run.tolerated_count === 1
                ? "1 step didn't run"
                : `${run.tolerated_count} steps didn't run`}
            </div>
          )}
      </div>
      <div className="shrink-0 text-xs text-muted">
        {run.stopped_by_user
          ? "stopped"
          : run.declined_by_user
            ? "declined"
            : run.nothing_to_do
              ? "nothing to do"
              : run.status.replace("_", " ")}
      </div>
    </button>
  );
}

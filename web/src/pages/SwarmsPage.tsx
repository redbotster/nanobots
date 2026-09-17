import { lazy, Suspense, useEffect, useState } from "react";
import { ImportSwarm, ImportSwarmButton } from "../components/ImportSwarm";
import { api } from "../lib/api";
import { listSwarmsCached } from "../lib/swarmsCache";
import { GettingStarted } from "../components/GettingStarted";
import type { StatusResponse, ComposeGap, SaveSwarmRequest, SwarmSummary } from "../lib/types";
import type { UIMode } from "../lib/uiMode";
import { SwarmView } from "./SwarmView";
import { Button } from "../components/Button";
import { StatusDot } from "../components/StatusDot";
import { relativeTime, untilTime } from "../lib/relativeTime";
import { swarmMatches } from "../lib/swarmFilter";

// Both are reached only by an explicit action — opening the builder, or
// opting into a foundry job after a composer gap — and the builder drags in
// the whole canvas/palette/inspector tree. Loading them lazily keeps them
// out of the bundle everyone downloads to look at a list of swarms.
const BuilderPage = lazy(() => import("./BuilderPage").then((m) => ({ default: m.BuilderPage })));
const FoundryJobPage = lazy(() =>
  import("./FoundryJobPage").then((m) => ({ default: m.FoundryJobPage })),
);

/** Deliberately quiet: these chunks load from the same local machine in a
 * few milliseconds, so a spinner would flash more than it informs. */
function LazyFallback() {
  return <div className="p-6 text-sm text-muted/60">Loading…</div>;
}

/** What this swarm's most recent run is doing, in words that are true while
 * it is doing it.
 *
 * Every state used to render as "last ran 2h ago", including the two where
 * the run has not finished. A swarm parked on an approval therefore looked
 * exactly like one that finished yesterday: same muted grey, same past
 * tense, nothing to suggest a person was being waited on.
 *
 * That is the failure mode this app actually has. Fifty-four runs on the
 * development machine died at their container timeout with an approval
 * nobody answered, 27 hours of container time, and the swarm card — the
 * thing you look at — said "last ran" the whole while. The nav badge does
 * count it, but it is a small number on a page you are not on.
 *
 * So the waiting case gets the warn colour and present tense, and "last
 * ran" is kept for runs that have actually finished.
 */
export function LastRunLine({ swarm: s }: { swarm: SwarmSummary }) {
  if (s.last_run_status === "awaiting_approval") {
    return (
      <div className="mt-1 flex items-center gap-1.5 text-[11px] font-medium text-warn">
        <StatusDot tone="warn" />
        waiting for your approval
      </div>
    );
  }
  if (s.last_run_status === "running") {
    return (
      <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted">
        <StatusDot tone="warn" />
        running now
      </div>
    );
  }
  if (!s.last_run_status) {
    return <div className="mt-1 text-[11px] text-muted/60">never run yet</div>;
  }
  return (
    <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted">
      <StatusDot tone={RUN_TONE[s.last_run_status] ?? "muted"} />
      last ran {relativeTime(s.last_run_at!)}
      {s.last_run_trigger === "schedule" && " · scheduled"}
      {s.last_run_trigger === "webhook" && " · by webhook"}
    </div>
  );
}

const RUN_TONE: Record<string, "ok" | "warn" | "danger" | "muted"> = {
  succeeded: "ok",
  running: "warn",
  awaiting_approval: "warn",
  failed: "danger",
  pending: "muted",
};

type Mode =
  | { kind: "list" }
  | { kind: "import" }
  | { kind: "view"; swarm: SwarmSummary }
  | { kind: "build"; swarm?: SwarmSummary; composedDraft?: SaveSwarmRequest }
  | { kind: "gap"; request: string; gap: ComposeGap }
  | { kind: "foundry"; jobId: string; request: string };

/** How often the swarm list re-checks. Four seconds: fast enough that a run
 * starting or finishing shows up while you are looking at the page, slow
 * enough that an idle list is one empty 304 round trip at a time. */
const SWARM_POLL_MS = 4000;

/** Below this many swarms, a filter box costs more attention than the
 * scrolling it saves. The catalog ships 16, so it is there by default; a
 * fresh install with three is not made better by a search field. */
const SEARCH_THRESHOLD = 6;

const EXAMPLE_PROMPT = "Help me automate a daily email recap and list it by priority";

/** The "head nanobot": describe what you want automated in plain English,
 * get a draft swarm back — already validated, never auto-saved (see
 * internal/api/compose.go). This is the primary, novice-friendly way to
 * start a swarm; the palette/canvas builder is still there for anyone who
 * wants to build or tweak by hand.
 *
 * A request the real catalog genuinely can't satisfy comes back as a gap
 * instead of a draft — onResult reports which one happened rather than
 * assuming success, so the caller can offer the foundry escalation. */
function ComposeBox({
  onResult,
}: {
  onResult: (message: string, result: { draft?: SaveSwarmRequest; gap?: ComposeGap }) => void;
}) {
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (!message.trim()) return;
    const trimmed = message.trim();
    setLoading(true);
    setError(null);
    try {
      const result = await api.compose(trimmed);
      onResult(trimmed, result);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="rounded-lg border border-edge-strong bg-panel p-5 shadow-glow-sm">
      <div className="flex items-center gap-2">
        <span className="text-lg">✨</span>
        <h2 id="compose-heading" className="font-display text-sm font-semibold text-ink">
          Describe what you want automated
        </h2>
      </div>
      <p className="mt-1 text-[13px] text-muted">
        Talk to the head nanobot — it snaps together a draft from the real catalog for you to
        review.
      </p>
      <div className="mt-3 flex flex-col gap-2 sm:flex-row">
        <input
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
          placeholder={EXAMPLE_PROMPT}
          disabled={loading}
          // The heading above is the label; pointing at it beats a second
          // copy of the same words. A placeholder is not a label — it is a
          // hint that disappears the moment you type.
          aria-labelledby="compose-heading"
          className="flex-1 rounded border border-edge-strong bg-void px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none disabled:opacity-60"
        />
        <Button variant="primary" onClick={submit} disabled={loading || !message.trim()}>
          {loading ? "Composing…" : "Automate it"}
        </Button>
      </div>
      {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
    </div>
  );
}

/** Shown when the composer declares a gap — the human opt-in gate before
 * the (slower, heavier) foundry ever runs. Never launched silently. */
function GapPanel({
  gap,
  onBuild,
  onDismiss,
}: {
  gap: ComposeGap;
  onBuild: () => void;
  onDismiss: () => void;
}) {
  return (
    <div className="rounded-lg border border-edge-strong bg-panel p-5 shadow-glow-sm">
      <h2 className="font-display text-sm font-semibold text-ink">The catalog can't do that yet</h2>
      <p className="mt-1 text-[13px] text-muted">
        Missing capability: <span className="text-ink">{gap.missing_capability}</span>
      </p>
      <p className="mt-2 text-[13px] text-muted">
        Want me to build a new bot for it? A sandboxed coding agent will author and self-test one —
        you review and approve it before it's ever part of the catalog.
      </p>
      <div className="mt-3 flex gap-2">
        <Button variant="primary" onClick={onBuild}>
          Build it
        </Button>
        <Button variant="ghost" onClick={onDismiss}>
          Never mind
        </Button>
      </div>
    </div>
  );
}

/** When a swarm fires on its own, and when that next happens. The scheduler
 * has been running these all along with nothing in the UI to say so — a
 * swarm that quietly emails you every weekday at 7 should not be a surprise.
 * A swarm with no cron trigger renders nothing rather than "manual only",
 * which would be noise on every card that has one. */

/** How real this swarm is right now.
 *
 * This used to read "0/4 live" on every card. On a fresh install nothing is
 * connected, so every one of fifteen cards showed a different-looking zero
 * — a scoreboard where every score is nil, which reads as "everything is
 * broken" while telling you nothing that distinguishes one card from
 * another. A fraction is only worth showing once it discriminates.
 *
 * So: nothing connected says "demo", quietly, because that is a state and
 * not a failure. Some connected shows the fraction, because now it is the
 * interesting number. All connected says "live". */
function ConnectionBadge({ swarm }: { swarm: SwarmSummary }) {
  const { services_live: live, services_total: total } = swarm;
  if (live === 0) {
    return (
      <span
        className="shrink-0 rounded-full border border-edge px-2 py-0.5 text-[10px] text-muted/70"
        title={`This swarm's ${total} service${total === 1 ? "" : "s"} all run on example data. Connect an account in Settings to make it real.`}
      >
        demo
      </span>
    );
  }
  const all = live === total;
  return (
    <span
      className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] ${
        all ? "border-ok/40 text-ok" : "border-warn/40 text-warn"
      }`}
      title={`${live} of ${total} services connected to a real account`}
    >
      {all ? "live" : `${live}/${total} live`}
    </span>
  );
}

/** A schedule that has stopped firing, and the one control that restarts it.
 *
 * A sibling of the card's own button rather than a child of it: a button
 * inside a button is invalid HTML, and stopPropagation only papers over
 * what that does to keyboard and assistive-tech users. */
function PausedNotice({ swarm, onResume }: { swarm: SwarmSummary; onResume: () => void }) {
  return (
    <div className="mx-4 mb-4 rounded border border-warn/40 bg-warn/5 px-2 py-1.5">
      <div className="text-[11px] font-medium text-warn">
        ⏸ Paused after {swarm.failure_streak} failed runs
      </div>
      {swarm.streak_error && (
        <div className="mt-0.5 line-clamp-2 text-[11px] leading-snug text-muted">
          {swarm.streak_error}
        </div>
      )}
      <button
        onClick={onResume}
        className="mt-1.5 rounded border border-edge bg-panel px-2 py-0.5 text-[11px] text-muted transition-colors hover:border-tron hover:text-ink"
      >
        Try it again
      </button>
    </div>
  );
}

export function ScheduleLine({ swarm }: { swarm: SwarmSummary }) {
  // A paused schedule outranks everything else this line could say — the
  // card renders PausedNotice below instead, so this stays quiet.
  if (swarm.schedule_paused) return null;
  if (swarm.schedule_error) {
    return (
      <div
        className="mt-2.5 text-[11px] text-danger"
        title={`${swarm.schedule_expr} — ${swarm.schedule_error}`}
      >
        ⏰ schedule can't be read, so this never fires on its own
      </div>
    );
  }
  if (swarm.inert_trigger) {
    return (
      <div
        className="mt-2.5 truncate text-[11px] text-muted/70"
        title={`This swarm declares a ${swarm.trigger_type} trigger (${swarm.inert_trigger}), which nothing in this build fires — so it runs only when you click Run.`}
      >
        ⚡ {swarm.inert_trigger} — not wired up yet, so runs on demand
      </div>
    );
  }
  if (swarm.trigger_type === "webhook") {
    // Not a schedule, but the same question — "does this run on its own?" —
    // and the answer is yes. Open it to get the URL.
    return (
      <div
        className="mt-2.5 truncate text-[11px] text-muted"
        title="Open this swarm to get its URL"
      >
        ⚡ Runs when something posts to it
      </div>
    );
  }
  if (!swarm.schedule) return null;
  return (
    // The streak sits outside the truncating span deliberately. When the
    // whole line truncated together, the failure warning was the first
    // thing cut — it is last in the line — so a narrow card showed
    // "Mondays at 8:00 AM · in 5d · 2 failed in ..." and dropped the only
    // part that was asking for attention. A warning outranks a schedule you
    // can read on the swarm's own page.
    <div
      className="mt-2.5 flex items-baseline gap-1 text-[11px] text-muted"
      title={`${swarm.schedule_expr}${swarm.timezone ? ` (${swarm.timezone})` : ""}`}
    >
      <span className="truncate">
        ⏰ {swarm.schedule}
        {swarm.next_run_at && (
          <span className="text-muted/70"> · {untilTime(swarm.next_run_at)}</span>
        )}
      </span>
      {/* A schedule plus an approval gate means this only works if someone
          is there when it fires. Seven of the sixteen catalog swarms are in
          that position and the card never said so — it showed a time and
          left you to find out from a run that died overnight. Not styled as
          a warning: it is how the swarm is built, and it is fine if you are
          around. */}
      {swarm.needs_approval && (
        <span
          className="shrink-0 text-muted/70"
          title="This swarm pauses for your approval when it runs. A scheduled run that nobody answers stops at the bot's timeout, so it only completes if you're there to say yes."
        >
          · pauses for you
        </span>
      )}
      {/* Said on the way down, not only once it has stopped — two failures
          in a row is worth knowing before the fifth. */}
      {(swarm.failure_streak ?? 0) > 1 && (
        <span className="shrink-0 text-warn">· {swarm.failure_streak} failed in a row</span>
      )}
    </div>
  );
}

export function SwarmsPage({
  uiMode,
  status,
  onOpenSettings,
}: {
  uiMode: UIMode;
  status: StatusResponse | null;
  onOpenSettings: () => void;
}) {
  const [swarms, setSwarms] = useState<SwarmSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<Mode>({ kind: "list" });
  const [query, setQuery] = useState("");

  const reload = () =>
    listSwarmsCached()
      .then(({ swarms }) => setSwarms(swarms))
      .catch((e) => setError(String(e)));

  // The landing page was a snapshot taken once on mount and never refreshed.
  // Everything live on it went stale the moment you arrived: a run that
  // finished still said "running now", a failure streak froze at whatever it
  // was when the page loaded, and a swarm that started waiting on an
  // approval while you were looking at it never said so — which is the one
  // thing this page most needs to be able to tell you.
  //
  // Conditional GET, not a plain poll: /api/swarms is ~10KB and almost every
  // poll is byte-identical, so the server answers 304 with no body and this
  // skips both the parse and the React state update (see getIfChanged). An
  // idle list costs one empty round trip every four seconds and does not
  // re-render.
  //
  // The tag lives in ../lib/swarmsCache rather than in this effect, because
  // this effect restarts every time you come back to the list — so the first
  // tick after a trip to Runs used to re-download all 10KB to be told what
  // it already had.
  //
  // Only while the list is on screen. The builder and the swarm view are
  // their own surfaces with their own loading, and polling underneath them
  // would be churn nobody can see.
  useEffect(() => {
    if (mode.kind !== "list") return;
    let alive = true;
    const tick = async () => {
      try {
        const res = await listSwarmsCached();
        if (!alive || !res.changed) return;
        setSwarms(res.swarms);
        setError(null);
      } catch (e) {
        // A failed poll is not worth replacing a good list with an error
        // message — the next tick usually succeeds, and a daemon restart
        // should not blank the page you are reading.
        if (alive && swarms === null) setError(String(e));
      }
    };
    tick();
    const id = setInterval(tick, SWARM_POLL_MS);
    return () => {
      alive = false;
      clearInterval(id);
    };
    // swarms is deliberately not a dependency: it changes on every poll that
    // brings something new, and restarting the interval each time would
    // reset the clock and re-request immediately.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode.kind]);

  // Filtered for display only — `swarms` stays the full list so the count in
  // the empty state below, and the threshold that decides whether to show the
  // filter at all, both describe what exists rather than what survived the
  // query.
  const visible = swarms === null ? null : swarms.filter((s) => swarmMatches(s, query));

  if (mode.kind === "view") {
    return (
      <SwarmView
        swarm={mode.swarm}
        onBack={() => setMode({ kind: "list" })}
        onEdit={() => setMode({ kind: "build", swarm: mode.swarm })}
      />
    );
  }

  if (mode.kind === "build") {
    return (
      <Suspense fallback={<LazyFallback />}>
        <BuilderPage
          existing={mode.swarm}
          composedDraft={mode.composedDraft}
          onDone={(savedPath) => {
            if (!savedPath) {
              setMode({ kind: "list" });
              reload();
              return;
            }
            listSwarmsCached().then(({ swarms: list }) => {
              setSwarms(list);
              const found = list.find((s) => s.path === savedPath);
              setMode(found ? { kind: "view", swarm: found } : { kind: "list" });
            });
          }}
        />
      </Suspense>
    );
  }

  if (mode.kind === "foundry") {
    const request = mode.request;
    return (
      <Suspense fallback={<LazyFallback />}>
        <FoundryJobPage
          jobId={mode.jobId}
          onDone={() => setMode({ kind: "list" })}
          onPromoted={() => {
            // The gap is filled — retry the exact same request with zero
            // further human input. It should succeed now.
            api.compose(request).then((result) => {
              setMode(
                result.draft ? { kind: "build", composedDraft: result.draft } : { kind: "list" },
              );
            });
          }}
        />
      </Suspense>
    );
  }

  return (
    <div className="h-full overflow-auto p-5 sm:p-6">
      <GettingStarted status={status} onOpenSettings={onOpenSettings} />
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-display text-xl font-medium text-ink">Swarms</h1>
          <p className="mt-1 hidden text-sm text-muted sm:block">
            Saved graphs of bots snapped together — pick one to see it run, live.
          </p>
        </div>
        {/* w-full below sm so the filter and the buttons get their own line
            rather than sharing one with the heading — three controls in a
            375px row is what made the button labels fold. */}
        <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">
          {(swarms?.length ?? 0) > SEARCH_THRESHOLD && (
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter swarms"
              aria-label="Filter swarms by name or description"
              className="w-full min-w-0 flex-1 rounded border border-edge-strong bg-void px-2.5 py-1.5 text-xs text-ink placeholder:text-muted focus:border-tron focus:outline-none sm:w-52 sm:flex-none"
            />
          )}
          <ImportSwarmButton onClick={() => setMode({ kind: "import" })} />
          {uiMode === "advanced" && (
            <Button variant="ghost" onClick={() => setMode({ kind: "build" })}>
              Build manually
            </Button>
          )}
        </div>
      </div>

      <div className="mt-4">
        {mode.kind === "import" ? (
          <ImportSwarm onImported={reload} onClose={() => setMode({ kind: "list" })} />
        ) : mode.kind === "gap" ? (
          <GapPanel
            gap={mode.gap}
            onDismiss={() => setMode({ kind: "list" })}
            onBuild={() => {
              const { request, gap } = mode;
              api
                .startFoundryJob(
                  request,
                  gap.missing_capability,
                  gap.suggested_inputs,
                  gap.suggested_outputs,
                )
                .then((job) => setMode({ kind: "foundry", jobId: job.id, request }))
                .catch((e) => setError(String(e)));
            }}
          />
        ) : (
          <ComposeBox
            onResult={(request, result) => {
              if (result.gap) {
                setMode({ kind: "gap", request, gap: result.gap });
              } else if (result.draft) {
                setMode({ kind: "build", composedDraft: result.draft });
              }
            }}
          />
        )}
      </div>

      {error && (
        <div className="mt-6 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
          Couldn't load swarms: {error}
        </div>
      )}

      <div className="mt-5 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {visible?.map((s) => (
          <div
            key={s.path}
            className="fade-in group flex flex-col rounded-lg border border-edge-strong bg-panel transition-shadow focus-within:border-tron hover:border-tron hover:shadow-glow-sm"
          >
            <button
              onClick={() => setMode({ kind: "view", swarm: s })}
              className="flex-1 p-4 text-left outline-none"
            >
              <div className="flex items-start justify-between gap-2">
                <h2 className="font-display text-base font-semibold text-ink">{s.name}</h2>
                {s.services_total > 0 && <ConnectionBadge swarm={s} />}
              </div>
              <p className="mt-1.5 text-[13px] leading-snug text-muted">{s.description}</p>
              <ScheduleLine swarm={s} />
              <LastRunLine swarm={s} />
            </button>
            {s.schedule_paused && (
              <PausedNotice
                swarm={s}
                onResume={() =>
                  api
                    .resumeSchedule(s.name)
                    .then(reload)
                    .catch((e) => setError(String(e)))
                }
              />
            )}
          </div>
        ))}
      </div>

      {swarms === null && !error && <p className="mt-5 text-sm text-muted/60">Loading swarms…</p>}
      {swarms?.length === 0 && (
        <p className="mt-6 text-sm text-muted">No swarms found in examples/swarms/.</p>
      )}
      {/* Filtered everything away. Distinct from having no swarms at all,
          which is a different problem with a different fix. */}
      {swarms !== null && swarms.length > 0 && visible!.length === 0 && (
        <p className="mt-6 text-sm text-muted">
          No swarm matches “{query}”.{" "}
          <button onClick={() => setQuery("")} className="text-tron hover:underline">
            Clear the filter
          </button>{" "}
          to see all {swarms.length}.
        </p>
      )}
    </div>
  );
}

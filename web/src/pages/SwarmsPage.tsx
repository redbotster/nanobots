import { lazy, Suspense, useEffect, useState } from "react";
import { ImportSwarm } from "../components/ImportSwarm";
import { api } from "../lib/api";
import { GettingStarted } from "../components/GettingStarted";
import type { StatusResponse, ComposeGap, SaveSwarmRequest, SwarmSummary } from "../lib/types";
import type { UIMode } from "../lib/uiMode";
import { SwarmView } from "./SwarmView";
import { Button } from "../components/Button";
import { StatusDot } from "../components/StatusDot";
import { relativeTime, untilTime } from "../lib/relativeTime";

// Both are reached only by an explicit action — opening the builder, or
// opting into a foundry job after a composer gap — and the builder drags in
// the whole canvas/palette/inspector tree. Loading them lazily keeps them
// out of the bundle everyone downloads to look at a list of swarms.
const BuilderPage = lazy(() => import("./BuilderPage").then((m) => ({ default: m.BuilderPage })));
const FoundryJobPage = lazy(() => import("./FoundryJobPage").then((m) => ({ default: m.FoundryJobPage })));

/** Deliberately quiet: these chunks load from the same local machine in a
 * few milliseconds, so a spinner would flash more than it informs. */
function LazyFallback() {
  return <div className="p-6 text-sm text-muted/60">Loading…</div>;
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
  | { kind: "view"; swarm: SwarmSummary }
  | { kind: "build"; swarm?: SwarmSummary; composedDraft?: SaveSwarmRequest }
  | { kind: "gap"; request: string; gap: ComposeGap }
  | { kind: "foundry"; jobId: string; request: string };

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
        <h2 className="font-display text-sm font-semibold text-ink">
          Describe what you want automated
        </h2>
      </div>
      <p className="mt-1 text-[13px] text-muted">
        Talk to the head nanobot — it snaps together a draft from the real catalog for you to review.
      </p>
      <div className="mt-3 flex flex-col gap-2 sm:flex-row">
        <input
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
          placeholder={EXAMPLE_PROMPT}
          disabled={loading}
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
      <h2 className="font-display text-sm font-semibold text-ink">
        The catalog can't do that yet
      </h2>
      <p className="mt-1 text-[13px] text-muted">
        Missing capability: <span className="text-ink">{gap.missing_capability}</span>
      </p>
      <p className="mt-2 text-[13px] text-muted">
        Want me to build a new bot for it? A sandboxed coding agent will
        author and self-test one — you review and approve it before it's
        ever part of the catalog.
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
function ScheduleLine({ swarm }: { swarm: SwarmSummary }) {
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
      <div className="mt-2.5 truncate text-[11px] text-muted" title="Open this swarm to get its URL">
        ⚡ Runs when something posts to it
      </div>
    );
  }
  if (!swarm.schedule) return null;
  return (
    <div
      className="mt-2.5 truncate text-[11px] text-muted"
      title={`${swarm.schedule_expr}${swarm.timezone ? ` (${swarm.timezone})` : ""}`}
    >
      ⏰ {swarm.schedule}
      {swarm.next_run_at && (
        <span className="text-muted/70"> · {untilTime(swarm.next_run_at)}</span>
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

  const reload = () => api.listSwarms().then(setSwarms).catch((e) => setError(String(e)));
  useEffect(() => {
    reload();
  }, []);

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
          api.listSwarms().then((list) => {
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
            setMode(result.draft ? { kind: "build", composedDraft: result.draft } : { kind: "list" });
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
        <div className="flex items-center gap-2">
          <ImportSwarm onImported={reload} />
          {uiMode === "advanced" && (
            <Button variant="ghost" onClick={() => setMode({ kind: "build" })}>
              Build manually
            </Button>
          )}
        </div>
      </div>

      <div className="mt-4">
        {mode.kind === "gap" ? (
          <GapPanel
            gap={mode.gap}
            onDismiss={() => setMode({ kind: "list" })}
            onBuild={() => {
              const { request, gap } = mode;
              api
                .startFoundryJob(request, gap.missing_capability, gap.suggested_inputs, gap.suggested_outputs)
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
        {swarms?.map((s) => (
          <button
            key={s.path}
            onClick={() => setMode({ kind: "view", swarm: s })}
            className="fade-in rounded-lg border border-edge-strong bg-panel p-4 text-left transition-shadow hover:border-tron hover:shadow-glow-sm"
          >
            <div className="flex items-start justify-between gap-2">
              <h2 className="font-display text-base font-semibold text-ink">
                {s.name}
              </h2>
              {s.services_total > 0 && (
                <span
                  className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] ${
                    s.services_live === 0
                      ? "border-edge text-muted"
                      : s.services_live === s.services_total
                        ? "border-ok/40 text-ok"
                        : "border-warn/40 text-warn"
                  }`}
                  title={`${s.services_live} of ${s.services_total} services connected to a real account`}
                >
                  {s.services_live}/{s.services_total} live
                </span>
              )}
            </div>
            <p className="mt-1.5 text-[13px] leading-snug text-muted">
              {s.description}
            </p>
            <ScheduleLine swarm={s} />
            {s.last_run_status ? (
              <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted">
                <StatusDot tone={RUN_TONE[s.last_run_status] ?? "muted"} />
                last ran {relativeTime(s.last_run_at!)}
                {s.last_run_trigger === "schedule" && " · scheduled"}
              </div>
            ) : (
              <div className="mt-1 text-[11px] text-muted/60">never run yet</div>
            )}
          </button>
        ))}
      </div>

      {swarms === null && !error && (
        <p className="mt-5 text-sm text-muted/60">Loading swarms…</p>
      )}
      {swarms?.length === 0 && (
        <p className="mt-6 text-sm text-muted">
          No swarms found in examples/swarms/.
        </p>
      )}
    </div>
  );
}

import { useEffect, useMemo, useRef, useState } from "react";
import { WebhookPanel } from "../components/WebhookPanel";
import { api } from "../lib/api";
import { useRun } from "../lib/useRun";
import type { BotSummary, PlanResult, SwarmSummary } from "../lib/types";
import { BotBrick } from "../components/BotBrick";
import { SnapTrail } from "../components/SnapTrail";
import { Sheet } from "../components/Sheet";
import { Inspector } from "../components/Inspector";
import { Tabs } from "../components/Tabs";
import { RunLog } from "../components/RunLog";
import { RunResults } from "../components/RunResults";
import { YamlView } from "../components/YamlView";
import { Button } from "../components/Button";
import { untilTime } from "../lib/relativeTime";
import { StatusDot } from "../components/StatusDot";

export function SwarmView({
  swarm,
  onBack,
  onEdit,
}: {
  swarm: SwarmSummary;
  onBack: () => void;
  onEdit?: () => void;
}) {
  const SWARM_PATH = swarm.path;
  const [plan, setPlan] = useState<PlanResult | null>(null);
  const [bots, setBots] = useState<Record<string, BotSummary>>({});
  const [selected, setSelected] = useState<string | null>(null);
  const [runId, setRunId] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState("log");

  const { run } = useRun(runId);

  // Jump to Results the moment a run succeeds — someone watching the log
  // shouldn't have to go hunting for what they actually got. Only fires on
  // the transition into "succeeded" (via the ref below), so manually
  // clicking back to the log afterward sticks instead of being fought.
  const wasSucceeded = useRef(false);
  useEffect(() => {
    if (run?.status === "succeeded" && !wasSucceeded.current) {
      setActiveTab("results");
    }
    wasSucceeded.current = run?.status === "succeeded";
  }, [run?.status]);

  useEffect(() => {
    setPlan(null);
    setSelected(null);
    // Adopt the swarm's most recent run rather than starting blank.
    //
    // This page used to know only about runs you started from it, so opening
    // a swarm always said "Nothing's run yet. Hit Run once above to see it
    // happen here, live" — including for a swarm whose run was at that
    // moment sitting on an approval. Following "waiting for your approval"
    // from the swarm list landed you on a page claiming nothing had ever
    // run, with no way to answer from there. The one action you came to take
    // was the one thing missing, and the copy was plainly false: the nav
    // badge beside it was counting that very run.
    setRunId(swarm.last_run_id ?? null);
    // A run that had already finished before this page opened must not
    // trigger the jump-to-Results below, which exists for the moment a run
    // you are watching succeeds — not for arriving at an old one.
    wasSucceeded.current = swarm.last_run_status === "succeeded";
    setLoadError(null);
    // Both of these used to swallow their failures — plan into an empty
    // catch, listBots into nothing at all — so a 500 from either left the
    // page showing "Loading swarm…" forever with Run once still enabled.
    api
      .plan(SWARM_PATH)
      .then(setPlan)
      .catch((e) => setLoadError(String(e)));
    api
      .listBots()
      .then((list) => setBots(Object.fromEntries(list.map((b) => [b.id, b]))))
      .catch((e) => setLoadError(String(e)));
  }, [SWARM_PATH, swarm.last_run_id, swarm.last_run_status]);

  const order = plan?.order ?? [];
  const botIdOf = useMemo(() => {
    const m = new Map<string, string>();
    for (const b of plan?.bots ?? []) m.set(b.instance_id, b.bot_id);
    return m;
  }, [plan]);
  const isBusy = run?.status === "running" || run?.status === "awaiting_approval";

  const activityOf = (botId: string): "idle" | "running" | "done" | "failed" => {
    if (!run) return "idle";
    const touched = run.log.some((l) => l.bot === botId);
    if (!touched) return "idle";
    const doneMarker = run.log.some((l) => l.bot === botId && l.msg === "done");
    if (run.status === "failed" && !doneMarker) return "failed";
    if (doneMarker) return "done";
    return "running";
  };

  const runOnce = async () => {
    setStarting(true);
    setStartError(null);
    setActiveTab("log");
    try {
      const r = await api.startRun(SWARM_PATH);
      setRunId(r.id);
    } catch (e) {
      setStartError(String(e));
    } finally {
      setStarting(false);
    }
  };

  // Which snap(s) actually connect two consecutive bots in the run order —
  // not just "the swarm's first snap," which broke down the moment a swarm
  // had more than 2 bots (every connector showed the same label).
  const snapsBetween = (fromInstance: string, toInstance: string) =>
    (plan?.snaps ?? []).filter(
      (s) => s.From.split(".")[0] === fromInstance && s.To.split(".")[0] === toInstance,
    );

  return (
    <div className="grid h-full grid-rows-[1fr_260px] sm:grid-rows-[1fr_280px]">
      <section className="overflow-auto p-5 sm:p-6">
        <button
          onClick={onBack}
          className="mb-3 flex items-center gap-1 font-display text-xs text-muted hover:text-ink"
        >
          ← Swarms
        </button>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 className="font-display text-xl font-medium text-ink">
              {plan?.swarm ?? swarm.name}
            </h1>
            <p className="mt-1 max-w-xl text-sm text-muted">{swarm.description}</p>
            {swarm.schedule && (
              <p className="mt-1.5 text-[12px] text-muted">
                ⏰ Runs automatically {swarm.schedule.toLowerCase()}
                {swarm.timezone && ` (${swarm.timezone})`}
                {swarm.next_run_at && ` — next ${untilTime(swarm.next_run_at)}`}
                <span className="ml-1.5 text-muted/60">{swarm.schedule_expr}</span>
              </p>
            )}
            {swarm.trigger_type === "webhook" && <WebhookPanel swarm={swarm} />}
            {swarm.schedule_error && (
              <p className="mt-1.5 text-[12px] text-danger">
                ⏰ This swarm's schedule can't be read ({swarm.schedule_expr}), so it
                never fires on its own: {swarm.schedule_error}
              </p>
            )}
          </div>
          <div className="flex items-center gap-2">
            {/* A download rather than a fetch-then-blob: the response is
                already a file with a Content-Disposition on it, and the
                browser does this better than we would. */}
            <a
              href={`/api/swarms/export?path=${encodeURIComponent(swarm.path)}`}
              download
              className="rounded-lg border border-edge px-3 py-1.5 font-display text-sm text-muted transition-colors hover:border-tron hover:text-ink"
              title="Download this swarm as a bundle you can send to someone — the file, the bots it needs, and what it can write to when it runs"
            >
              Share
            </a>
            {onEdit && (
              <Button variant="ghost" onClick={onEdit} disabled={isBusy}>
                Edit
              </Button>
            )}
          </div>
        </div>

        <div className="mt-3 flex items-center gap-3">
          <Button variant="primary" onClick={runOnce} disabled={starting || isBusy}>
            {isBusy ? "Running…" : "Run once"}
          </Button>
          {run && (
            <span className="flex items-center gap-1.5 text-xs text-muted">
              <StatusDot
                tone={
                  run.status === "succeeded"
                    ? "ok"
                    : run.status === "failed"
                      ? "danger"
                      : "warn"
                }
              />
              {run.status.replace("_", " ")}
            </span>
          )}
          {startError && <span className="text-xs text-danger">{startError}</span>}
        </div>

        <div className="mt-8 flex flex-wrap items-center gap-0 sm:flex-nowrap">
          {order.map((instanceId, i) => {
            const bot = bots[botIdOf.get(instanceId) ?? ""];
            const nextId = order[i + 1];
            const snaps = nextId ? snapsBetween(instanceId, nextId) : [];
            return (
              <div key={instanceId} className="flex items-center">
                {bot && (
                  <BotBrick
                    bot={bot}
                    active={selected === instanceId}
                    activity={activityOf(instanceId)}
                    onClick={() => setSelected(instanceId)}
                  />
                )}
                {i < order.length - 1 && (
                  <SnapTrail
                    from={snaps[0] ? snaps[0].From.split(".").slice(1).join(".") : "output"}
                    to={snaps[0] ? snaps[0].To.split(".").slice(1).join(".") : "input"}
                    extra={Math.max(0, snaps.length - 1)}
                    join={snaps[0]?.join}
                    live={run?.status === "running"}
                  />
                )}
              </div>
            );
          })}
          {order.length === 0 &&
            (loadError ? (
              <div className="max-w-xl rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
                Couldn't load this swarm: {loadError}
              </div>
            ) : (
              <div className="text-sm text-muted">Loading swarm…</div>
            ))}
        </div>

        {plan && !plan.ok && (
          <div className="mt-6 max-w-xl rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
            This swarm doesn't type-check yet — fix the ports, not the
            planner.
          </div>
        )}
      </section>

      <section className="border-t border-edge bg-panel/40">
        <Tabs
          defaultValue="log"
          value={activeTab}
          onValueChange={setActiveTab}
          right={run ? `run ${run.id.slice(0, 8)}` : undefined}
          tabs={[
            { value: "log", label: "Run log", content: <RunLog run={run} /> },
            { value: "results", label: "Results", content: <RunResults run={run} /> },
            {
              value: "yaml",
              label: "nanoswarm.yaml",
              content: <YamlView path={SWARM_PATH} />,
            },
          ]}
        />
      </section>

      {selected && bots[botIdOf.get(selected) ?? ""] && (
        <Sheet
          open={!!selected}
          onOpenChange={(open) => !open && setSelected(null)}
          title={bots[botIdOf.get(selected)!].name}
          subtitle={`${selected} · ${bots[botIdOf.get(selected)!].id}@${bots[botIdOf.get(selected)!].version}`}
        >
          <Inspector bot={bots[botIdOf.get(selected)!]} />
        </Sheet>
      )}
    </div>
  );
}

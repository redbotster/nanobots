import { useEffect, useMemo, useState } from "react";
import { api } from "../lib/api";
import { useRun } from "../lib/useRun";
import type { BotSummary, PlanResult, SwarmSummary } from "../lib/types";
import { BotBrick } from "../components/BotBrick";
import { SnapTrail } from "../components/SnapTrail";
import { Sheet } from "../components/Sheet";
import { Inspector } from "../components/Inspector";
import { Tabs } from "../components/Tabs";
import { RunLog } from "../components/RunLog";
import { YamlView } from "../components/YamlView";
import { Button } from "../components/Button";
import { StatusDot } from "../components/StatusDot";

export function SwarmView({
  swarm,
  onBack,
}: {
  swarm: SwarmSummary;
  onBack: () => void;
}) {
  const SWARM_PATH = swarm.path;
  const [plan, setPlan] = useState<PlanResult | null>(null);
  const [bots, setBots] = useState<Record<string, BotSummary>>({});
  const [selected, setSelected] = useState<string | null>(null);
  const [runId, setRunId] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);

  const { run } = useRun(runId);

  useEffect(() => {
    setPlan(null);
    setSelected(null);
    setRunId(null);
    api.plan(SWARM_PATH).then(setPlan).catch(() => {});
    api.listBots().then((list) => {
      setBots(Object.fromEntries(list.map((b) => [b.id, b])));
    });
  }, [SWARM_PATH]);

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
    try {
      const r = await api.startRun(SWARM_PATH);
      setRunId(r.id);
    } catch (e) {
      setStartError(String(e));
    } finally {
      setStarting(false);
    }
  };

  const snapLabel = useMemo(() => {
    const s = plan?.snaps?.[0];
    return s ? { from: s.From.split(".")[1], to: s.To.split(".")[1] } : null;
  }, [plan]);

  return (
    <div className="grid h-full grid-rows-[1fr_260px] sm:grid-rows-[1fr_280px]">
      <section className="overflow-auto p-6 sm:p-8">
        <button
          onClick={onBack}
          className="mb-3 flex items-center gap-1 font-display text-xs text-muted hover:text-ink"
        >
          ← Swarms
        </button>
        <h1 className="font-display text-xl font-medium text-ink">
          {plan?.swarm ?? swarm.name}
        </h1>
        <p className="mt-1 max-w-xl text-sm text-muted">{swarm.description}</p>

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
                    from={snapLabel?.from ?? "output"}
                    to={snapLabel?.to ?? "input"}
                    live={run?.status === "running"}
                  />
                )}
              </div>
            );
          })}
          {order.length === 0 && (
            <div className="text-sm text-muted">Loading swarm…</div>
          )}
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
          right={run ? `run ${run.id.slice(0, 8)}` : undefined}
          tabs={[
            { value: "log", label: "Run log", content: <RunLog run={run} /> },
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

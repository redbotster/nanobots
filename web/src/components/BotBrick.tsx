import type { BotSummary } from "../lib/types";
import { PortBadge } from "./PortBadge";

const EMPTY_SET: ReadonlySet<string> = new Set();

export function BotBrick({
  bot,
  active,
  activity,
  onClick,
  wiredInputs = EMPTY_SET,
  wiredOutputs = EMPTY_SET,
  unfedInputs = EMPTY_SET,
}: {
  bot: BotSummary;
  active: boolean;
  activity?: "idle" | "running" | "done" | "failed";
  onClick: () => void;
  /** Which of this bot's own ports are actually fed by a real snap in
   * this swarm, and which required inputs have nothing feeding them at
   * all — computed once in SwarmView from the same plan.snaps/plan.unfed
   * the connector lines between bots already use, so a port's dot and
   * the wire drawn beside it can never disagree about what's connected.
   * Left empty (the default) shows every port as plain "not connected",
   * the same look this component always had before wiring state existed. */
  wiredInputs?: ReadonlySet<string>;
  wiredOutputs?: ReadonlySet<string>;
  unfedInputs?: ReadonlySet<string>;
}) {
  const ring =
    activity === "running"
      ? "border-tron shadow-glow animate-pulse"
      : activity === "failed"
        ? "border-danger"
        : activity === "done"
          ? "border-ok"
          : active
            ? "border-tron shadow-glow-sm"
            : "border-edge-strong hover:border-tron/60";

  return (
    <button
      onClick={onClick}
      className={`fade-in relative w-full max-w-[280px] rounded-lg border bg-panel px-4 pb-4 pt-3.5 text-left transition-all ${ring}`}
    >
      <div className="flex items-center justify-between font-display text-[11px] tracking-wide text-tron">
        {bot.id}
        <span className="text-muted">v{bot.version}</span>
      </div>
      <h3 className="mt-1 font-display text-[15px] font-semibold text-ink">{bot.name}</h3>
      <p className="mt-1 line-clamp-2 text-[12.5px] leading-snug text-muted">{bot.description}</p>
      <div className="mt-2.5 flex flex-wrap gap-1">
        {(bot.services ?? []).map((s) => (
          <span
            key={s.id}
            className="rounded border border-edge px-1.5 py-0.5 text-[10px] text-ink"
          >
            {s.id}
          </span>
        ))}
        <span className="rounded border border-edge px-1.5 py-0.5 text-[10px] text-muted">
          {bot.harness}
        </span>
      </div>

      <div className="pointer-events-none absolute -left-1.5 top-1/2 flex -translate-y-1/2 flex-col gap-2.5">
        {(bot.inputs ?? []).map((p) => (
          <PortBadge
            key={p.name}
            name={p.name}
            type={p.type}
            state={
              unfedInputs.has(p.name) ? "unfed" : wiredInputs.has(p.name) ? "wired" : "unwired"
            }
          />
        ))}
      </div>
      <div className="pointer-events-none absolute -right-1.5 top-1/2 flex -translate-y-1/2 flex-col gap-2.5">
        {(bot.outputs ?? []).map((p) => (
          <PortBadge
            key={p.name}
            name={p.name}
            type={p.type}
            state={wiredOutputs.has(p.name) ? "wired" : "unwired"}
          />
        ))}
      </div>
    </button>
  );
}

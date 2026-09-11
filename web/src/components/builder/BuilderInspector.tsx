import type { BotSummary } from "../../lib/types";
import type { CanvasSnap, PlacedBot } from "./BuilderCanvas";

/** The selected node's settings panel: a text field per input port that
 * isn't fed by a snap. Every value is kept and sent as a plain string —
 * matching how nanobot.yaml `default:` values are themselves always written
 * as literals — rather than building a per-type editor for booleans/json/
 * datetime; a real gap for anyone needing, say, a literal JSON object here,
 * not hidden, just not built yet. */
export function BuilderInspector({
  bot,
  def,
  snaps,
  values,
  onChange,
  onClose,
}: {
  bot: PlacedBot;
  def: BotSummary;
  snaps: CanvasSnap[];
  values: Record<string, string>;
  onChange: (port: string, value: string) => void;
  onClose: () => void;
}) {
  return (
    <div className="flex h-full w-64 shrink-0 flex-col overflow-auto border-l border-edge">
      <div className="flex items-start justify-between gap-2 border-b border-edge p-4">
        <div className="min-w-0">
          <h3 className="truncate font-display text-sm font-semibold text-ink">{def.name}</h3>
          <p className="mt-0.5 truncate text-[11px] text-muted">
            {bot.instanceId} · {def.id}@{def.version}
          </p>
        </div>
        <button onClick={onClose} className="shrink-0 text-muted hover:text-ink" title="Close">
          ✕
        </button>
      </div>

      <div className="space-y-3 p-4">
        {def.inputs.map((p) => {
          const wiredFrom = snaps.find((s) => s.to === `${bot.instanceId}.${p.name}`);
          return (
            <div key={p.name}>
              <label className="flex flex-wrap items-baseline gap-1 text-[11px] font-medium text-ink">
                {p.name}
                {p.required && !p.default && <span className="text-danger">*</span>}
                <span className="font-normal text-muted">({p.type})</span>
              </label>
              {wiredFrom ? (
                <div className="mt-1 truncate rounded border border-edge bg-void px-2 py-1.5 text-[11px] text-muted">
                  wired from {wiredFrom.from}
                </div>
              ) : (
                <input
                  value={values[p.name] ?? ""}
                  onChange={(e) => onChange(p.name, e.target.value)}
                  placeholder={p.default ?? (p.required ? "required" : "optional")}
                  className="mt-1 w-full rounded border border-edge-strong bg-void px-2 py-1.5 text-[11px] text-ink placeholder:text-muted/60 focus:border-tron focus:outline-none"
                />
              )}
            </div>
          );
        })}
        {def.inputs.length === 0 && (
          <p className="text-[11px] text-muted">This bot has no inputs.</p>
        )}
      </div>
    </div>
  );
}

import { useState } from "react";
import type { BotSummary } from "../../lib/types";
import type { CanvasSnap, PlacedBot } from "./BuilderCanvas";

/** A wired input's base connection ("recap.recap_json") plus an editable
 * suffix for picking a nested field of a json/list<json> output
 * ("headline", ".0.subject", ...) — the canvas itself only ever draws
 * whole-port-to-port lines (a fresh one replaces the base, see
 * BuilderCanvas's own doc comment), so this is the one place a human can
 * reach the same dotted-field-path connections that were previously only
 * possible by hand-editing the saved YAML (e.g.
 * daily-email-recap.yaml's recap.recap_json.headline). A bad field name
 * isn't validated here — it surfaces as a real type-mismatch error from
 * the same planner a save goes through, the same way any other bad snap
 * already does. */
function WiredFromField({
  from,
  onChange,
}: {
  from: string;
  onChange: (newFrom: string) => void;
}) {
  const parts = from.split(".");
  const base = parts.slice(0, 2).join(".");
  const [suffix, setSuffix] = useState(parts.slice(2).join("."));

  const commit = () => {
    const trimmed = suffix.trim().replace(/^\.+|\.+$/g, "");
    onChange(trimmed ? `${base}.${trimmed}` : base);
  };

  return (
    <div className="mt-1 rounded border border-edge bg-void px-2 py-1.5">
      <div className="truncate text-[11px] text-muted">wired from {base}</div>
      <input
        value={suffix}
        onChange={(e) => setSuffix(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === "Enter" && (e.currentTarget as HTMLInputElement).blur()}
        placeholder="optional field, e.g. headline or 0.subject"
        className="mt-1 w-full rounded border border-edge-strong bg-panel px-1.5 py-1 text-[11px] text-ink placeholder:text-muted/60 focus:border-tron focus:outline-none"
      />
    </div>
  );
}

/** The selected node's settings panel: a text field per input port that
 * isn't fed by a snap (or a nested-field editor for one that is) — matching
 * how nanobot.yaml `default:` values are themselves always written as
 * literals — rather than building a per-type editor for booleans/json/
 * datetime; a real gap for anyone needing, say, a literal JSON object here,
 * not hidden, just not built yet. */
export function BuilderInspector({
  bot,
  def,
  snaps,
  values,
  onChange,
  onEditSnapFrom,
  onClose,
}: {
  bot: PlacedBot;
  def: BotSummary;
  snaps: CanvasSnap[];
  values: Record<string, string>;
  onChange: (port: string, value: string) => void;
  onEditSnapFrom: (snapIndex: number, newFrom: string) => void;
  onClose: () => void;
}) {
  return (
    <div className="flex h-full w-full shrink-0 flex-col overflow-auto border-edge sm:w-64 sm:border-l">
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
          const wiredIndex = snaps.findIndex((s) => s.to === `${bot.instanceId}.${p.name}`);
          const wiredFrom = wiredIndex >= 0 ? snaps[wiredIndex] : undefined;
          return (
            <div key={p.name}>
              <label className="flex flex-wrap items-baseline gap-1 text-[11px] font-medium text-ink">
                {p.name}
                {p.required && !p.default && <span className="text-danger">*</span>}
                <span className="font-normal text-muted">({p.type})</span>
              </label>
              {wiredFrom ? (
                <WiredFromField
                  // Keyed by the current value so an external rewire (e.g.
                  // dragging a new connection onto this same port while
                  // this panel is open) resets the suffix input's local
                  // state instead of showing a stale edit.
                  key={wiredFrom.from}
                  from={wiredFrom.from}
                  onChange={(newFrom) => onEditSnapFrom(wiredIndex, newFrom)}
                />
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

import { useState } from "react";
import type { BotSummary } from "../../lib/types";

/** The bot catalog, click-to-add onto the canvas. Search is client-side
 * (the catalog is small) matching id/name/description/tags. */
export function BuilderPalette({
  bots,
  onAdd,
}: {
  bots: BotSummary[];
  onAdd: (bot: BotSummary) => void;
}) {
  const [q, setQ] = useState("");
  const query = q.trim().toLowerCase();
  const filtered = bots.filter((b) =>
    !query ||
    b.id.includes(query) ||
    b.name.toLowerCase().includes(query) ||
    b.description.toLowerCase().includes(query) ||
    b.tags.some((t) => t.toLowerCase().includes(query)),
  );

  return (
    <div className="flex h-full min-h-0 flex-col border-r border-edge">
      <div className="shrink-0 border-b border-edge p-3">
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search bots…"
          className="w-full rounded border border-edge-strong bg-void px-2.5 py-1.5 text-xs text-ink placeholder:text-muted focus:border-tron focus:outline-none"
        />
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {filtered.map((bot) => (
          <button
            key={bot.id}
            onClick={() => onAdd(bot)}
            className="mb-1.5 w-full rounded-md border border-edge-strong bg-panel p-2.5 text-left transition-colors hover:border-tron hover:bg-tron/5"
          >
            <div className="font-display text-xs font-semibold text-ink">{bot.name}</div>
            <div className="mt-0.5 line-clamp-2 text-[11px] leading-snug text-muted">
              {bot.description}
            </div>
            <div className="mt-1 flex flex-wrap gap-1">
              {bot.tags.slice(0, 3).map((t) => (
                <span
                  key={t}
                  className="rounded-full border border-edge px-1.5 py-0.5 text-[9px] text-muted"
                >
                  {t}
                </span>
              ))}
            </div>
          </button>
        ))}
        {filtered.length === 0 && (
          <p className="p-2 text-xs text-muted">No bots match "{q}".</p>
        )}
      </div>
    </div>
  );
}

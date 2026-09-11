import { useMemo, useState } from "react";
import type { BotSummary, ConnectionStatus } from "../../lib/types";
import { categoryOf } from "../../lib/botCategory";

/** Small "demo" / "live" / "not connected" pills per unique provider a bot
 * declares — "demo" reads the bot's own YAML default (every shipped bot
 * today), "live"/"not connected" only apply once a bot's connection: is
 * switched away from demo, cross-checked against whether that provider's
 * account is actually connected (from GET /api/connections). */
function ConnectionPills({ bot, connections }: { bot: BotSummary; connections: ConnectionStatus[] }) {
  const seen = new Set<string>();
  const pills = bot.services.filter((s) => {
    if (seen.has(s.provider)) return false;
    seen.add(s.provider);
    return true;
  });
  if (pills.length === 0) return null;
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {pills.map((s) => {
        const isDemo = (s.connection ?? "demo") === "demo";
        const live = connections.find((c) => c.service === s.provider)?.connected;
        const label = isDemo ? "demo" : live ? "live" : "not connected";
        const tone = isDemo ? "text-muted border-edge" : live ? "text-ok border-ok/40" : "text-warn border-warn/40";
        return (
          <span key={s.provider} className={`rounded-full border px-1.5 py-0.5 text-[9px] ${tone}`}>
            {s.provider} · {label}
          </span>
        );
      })}
    </div>
  );
}

/** The bot catalog, click-to-add onto the canvas, grouped by the service it
 * primarily talks to. Search is client-side (the catalog is small) matching
 * id/name/description/tags. */
export function BuilderPalette({
  bots,
  connections,
  onAdd,
}: {
  bots: BotSummary[];
  connections: ConnectionStatus[];
  onAdd: (bot: BotSummary) => void;
}) {
  const [q, setQ] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const query = q.trim().toLowerCase();
  const filtered = bots.filter(
    (b) =>
      !query ||
      b.id.includes(query) ||
      b.name.toLowerCase().includes(query) ||
      b.description.toLowerCase().includes(query) ||
      b.tags.some((t) => t.toLowerCase().includes(query)),
  );

  const groups = useMemo(() => {
    const m = new Map<string, BotSummary[]>();
    for (const bot of filtered) {
      const cat = categoryOf(bot);
      m.set(cat, [...(m.get(cat) ?? []), bot]);
    }
    return [...m.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [filtered]);

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
        {groups.map(([category, groupBots]) => (
          <div key={category} className="mb-2">
            <button
              onClick={() => setCollapsed((c) => ({ ...c, [category]: !c[category] }))}
              className="flex w-full items-center gap-1.5 px-1 py-1 text-left font-display text-[10px] font-semibold uppercase tracking-wider text-muted hover:text-ink"
            >
              <span className={`inline-block transition-transform ${collapsed[category] ? "-rotate-90" : ""}`}>▾</span>
              {category}
              <span className="ml-auto font-normal normal-case tracking-normal">{groupBots.length}</span>
            </button>
            {!collapsed[category] &&
              groupBots.map((bot) => (
                <button
                  key={bot.id}
                  onClick={() => onAdd(bot)}
                  className="mb-1.5 w-full rounded-md border border-edge-strong bg-panel p-2.5 text-left transition-colors hover:border-tron hover:bg-tron/5"
                >
                  <div className="font-display text-xs font-semibold text-ink">{bot.name}</div>
                  <div className="mt-0.5 line-clamp-2 text-[11px] leading-snug text-muted">
                    {bot.description}
                  </div>
                  <ConnectionPills bot={bot} connections={connections} />
                </button>
              ))}
          </div>
        ))}
        {filtered.length === 0 && (
          <p className="p-2 text-xs text-muted">No bots match "{q}".</p>
        )}
      </div>
    </div>
  );
}

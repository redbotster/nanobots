import { useEffect, useMemo, useState } from "react";
import { api } from "../lib/api";
import { listBotsCached } from "../lib/botsCache";
import type { BotSummary, ConnectionStatus } from "../lib/types";
import { categoryOf, prettyProvider } from "../lib/botCategory";
import { BotCard } from "../components/BotCard";

/** The pool for categories that would otherwise hold a single bot. Named
 * rather than inlined so the render can tell it apart — it is the one group
 * whose heading does not name a service. */
const OTHER = "One-offs";

export function BotLibrary() {
  const [bots, setBots] = useState<BotSummary[] | null>(null);
  const [connections, setConnections] = useState<ConnectionStatus[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const reload = () => {
    listBotsCached()
      .then(setBots)
      .catch((e) => setError(String(e)));
    api
      .listConnections()
      .then(setConnections)
      .catch(() => {});
  };
  useEffect(() => {
    reload();
  }, []);

  const query = q.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      (bots ?? []).filter(
        (b) =>
          !query ||
          b.id.includes(query) ||
          b.name.toLowerCase().includes(query) ||
          b.description.toLowerCase().includes(query) ||
          b.tags.some((t) => t.toLowerCase().includes(query)) ||
          // By service, too. post-publisher is findable as "linkedin" only
          // because its description happens to say so; a bot whose
          // description doesn't name its provider should still be findable
          // by it.
          b.services.some((svc) => svc.provider.toLowerCase().includes(query)),
      ),
    [bots, query],
  );

  const { groups, pooled } = useMemo(() => {
    const m = new Map<string, BotSummary[]>();
    for (const bot of filtered) {
      const cat = categoryOf(bot);
      m.set(cat, [...(m.get(cat) ?? []), bot]);
    }

    // A category holding one bot costs a heading plus a grid row with two
    // empty thirds, and there were four of them. Pooled into one group they
    // fill a single row between them — the card still names the service, and
    // search finds them by name either way.
    const singles: BotSummary[] = [];
    const singleCats: string[] = [];
    const kept: [string, BotSummary[]][] = [];
    for (const [cat, list] of m) {
      if (list.length === 1 && cat !== "Utility") {
        singles.push(list[0]);
        // Every provider these bots touch, not just the one they are filed
        // under. post-publisher posts to X *and* LinkedIn, and is filed
        // under X — so listing categories alone left "LinkedIn" appearing
        // nowhere on the page.
        for (const svc of list[0].services) singleCats.push(prettyProvider(svc.provider));
      } else kept.push([cat, list]);
    }

    // Biggest first. Alphabetical put GitHub's single bot above Google's
    // eighteen, so the page opened on its least useful group.
    kept.sort(([a, la], [b, lb]) => lb.length - la.length || a.localeCompare(b));
    if (singles.length > 0) {
      singles.sort((a, b) => a.id.localeCompare(b.id));
      kept.push([OTHER, singles]);
    }
    // Drop providers that already have a heading of their own. One pooled
    // bot (invoice-chaser) also touches Google, and listing "Google" here
    // next to a Google group of seventeen raises a question instead of
    // answering one. The heading exists to say what is in here that is
    // nowhere else.
    const elsewhere = new Set(kept.map(([cat]) => cat));
    const pooled = [...new Set(singleCats)].filter((p) => !elsewhere.has(p)).sort();
    return { groups: kept, pooled };
  }, [filtered]);

  return (
    <div className="h-full overflow-auto p-5 sm:p-6">
      <h1 className="font-display text-xl font-medium text-ink">Bot library</h1>
      <p className="mt-1 hidden text-sm text-muted sm:block">
        Every bot declares typed ports — any bot here can be snapped into a swarm you build,
        including ones nobody's thought of yet. Flip a service's switch to connect an account and
        make that bot run for real instead of on demo data.
      </p>

      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search by name, tag, or description…"
        aria-label="Search bots by name, tag, or description"
        className="mt-4 w-full max-w-sm rounded border border-edge-strong bg-void px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none sm:w-80"
      />

      {error && (
        <div className="mt-6 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
          Couldn't load bots: {error}
        </div>
      )}

      {groups.map(([category, groupBots]) => (
        <section key={category} className="mt-5">
          <button
            onClick={() => setCollapsed((c) => ({ ...c, [category]: !c[category] }))}
            className="flex items-center gap-1.5 py-1 font-display text-xs font-semibold uppercase tracking-wider text-muted hover:text-ink"
          >
            <span
              className={`inline-block transition-transform ${collapsed[category] ? "-rotate-90" : ""}`}
            >
              ▾
            </span>
            {category}
            <span className="font-normal normal-case tracking-normal">({groupBots.length})</span>
            {category === OTHER && (
              // Naming them is the whole point. Pooled under a bare
              // "One-offs", the only bot that posts to X and LinkedIn became
              // invisible to anyone scanning for X or LinkedIn.
              <span className="font-normal normal-case tracking-normal text-muted/60">
                {pooled.join(" · ")}
              </span>
            )}
          </button>
          {/* items-start below, so a card is as tall as its own content.
              Grid items stretch by default, which made a one-service bot
              with no instructions — drive-save — grow 130px of empty panel
              to match the tallest card in its row, and read as unfinished. */}
          {!collapsed[category] && (
            <div className="mt-2 grid grid-cols-1 items-start gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {groupBots.map((bot) => (
                <BotCard key={bot.id} bot={bot} connections={connections} onChanged={reload} />
              ))}
            </div>
          )}
        </section>
      ))}

      {bots === null && !error && <p className="mt-5 text-sm text-muted/60">Loading bots…</p>}
      {bots?.length === 0 && (
        <p className="mt-6 text-sm text-muted">No bots found in the bots/ directory.</p>
      )}
      {bots && bots.length > 0 && filtered.length === 0 && (
        <p className="mt-8 text-sm text-muted">No bots match "{q}".</p>
      )}
    </div>
  );
}

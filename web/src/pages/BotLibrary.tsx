import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { BotSummary } from "../lib/types";
import { PortBadge } from "../components/PortBadge";

export function BotLibrary() {
  const [bots, setBots] = useState<BotSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.listBots().then(setBots).catch((e) => setError(String(e)));
  }, []);

  return (
    <div className="h-full overflow-auto p-6 sm:p-8">
      <h1 className="font-display text-xl font-medium text-ink">Bot library</h1>
      <p className="mt-1 text-sm text-muted">
        Every bot declares typed ports — any bot here can be snapped into a
        swarm you build, including ones nobody's thought of yet.
      </p>

      {error && (
        <div className="mt-6 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
          Couldn't load bots: {error}
        </div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {bots?.map((bot) => (
          <div
            key={bot.id}
            className="fade-in rounded-lg border border-edge-strong bg-panel p-4 transition-shadow hover:shadow-glow-sm"
          >
            <div className="flex items-center justify-between font-display text-[11px] tracking-wide text-tron">
              {bot.id} <span className="text-muted">v{bot.version}</span>
            </div>
            <h2 className="mt-1 font-display text-base font-semibold text-ink">
              {bot.name}
            </h2>
            <p className="mt-1 text-[13px] leading-snug text-muted">
              {bot.description}
            </p>

            <div className="mt-3 flex flex-wrap gap-1.5">
              {(bot.services ?? []).map((s) => (
                <span
                  key={s.id}
                  className="rounded border border-edge px-2 py-0.5 text-[11px] text-ink"
                >
                  {s.id}
                  {s.connection === "demo" && (
                    <span className="text-warn"> · demo</span>
                  )}
                </span>
              ))}
              <span className="rounded border border-edge px-2 py-0.5 text-[11px] text-muted">
                {bot.harness}
              </span>
            </div>

            <div className="mt-3 flex items-center justify-between text-[11px] text-muted">
              <div className="flex items-center gap-1.5">
                {(bot.inputs ?? []).map((p) => (
                  <PortBadge key={p.name} name={p.name} type={p.type} dim />
                ))}
                <span>in</span>
              </div>
              <div className="flex items-center gap-1.5">
                <span>out</span>
                {(bot.outputs ?? []).map((p) => (
                  <PortBadge key={p.name} name={p.name} type={p.type} />
                ))}
              </div>
            </div>
          </div>
        ))}
      </div>

      {bots?.length === 0 && (
        <p className="mt-8 text-sm text-muted">
          No bots found in the bots/ directory.
        </p>
      )}
    </div>
  );
}

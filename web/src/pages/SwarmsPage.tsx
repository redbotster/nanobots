import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { SwarmSummary } from "../lib/types";
import { SwarmView } from "./SwarmView";

export function SwarmsPage() {
  const [swarms, setSwarms] = useState<SwarmSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<SwarmSummary | null>(null);

  useEffect(() => {
    api.listSwarms().then(setSwarms).catch((e) => setError(String(e)));
  }, []);

  if (selected) {
    return <SwarmView swarm={selected} onBack={() => setSelected(null)} />;
  }

  return (
    <div className="h-full overflow-auto p-6 sm:p-8">
      <h1 className="font-display text-xl font-medium text-ink">Swarms</h1>
      <p className="mt-1 text-sm text-muted">
        Saved graphs of bots snapped together — pick one to see it run, live.
      </p>

      {error && (
        <div className="mt-6 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
          Couldn't load swarms: {error}
        </div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {swarms?.map((s) => (
          <button
            key={s.path}
            onClick={() => setSelected(s)}
            className="fade-in rounded-lg border border-edge-strong bg-panel p-4 text-left transition-shadow hover:border-tron hover:shadow-glow-sm"
          >
            <h2 className="font-display text-base font-semibold text-ink">
              {s.name}
            </h2>
            <p className="mt-1.5 text-[13px] leading-snug text-muted">
              {s.description}
            </p>
          </button>
        ))}
      </div>

      {swarms?.length === 0 && (
        <p className="mt-8 text-sm text-muted">
          No swarms found in examples/swarms/.
        </p>
      )}
    </div>
  );
}

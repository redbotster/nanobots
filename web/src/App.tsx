import { useEffect, useState } from "react";
import { api } from "./lib/api";
import type { StatusResponse } from "./lib/types";
import { SwarmsPage } from "./pages/SwarmsPage";
import { BotLibrary } from "./pages/BotLibrary";
import { RunsPage } from "./pages/RunsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { StatusDot } from "./components/StatusDot";

type Page = "swarm" | "bots" | "runs" | "settings";

const NAV: { id: Page; label: string; icon: string }[] = [
  { id: "bots", label: "Bot library", icon: "M4 7h16M4 12h10M4 17h7" },
  {
    id: "swarm",
    label: "Swarms",
    icon: "M4 5h7v6H4zM13 13h7v6h-7zM10 8h2a2 2 0 0 1 2 2v3",
  },
  { id: "runs", label: "Runs", icon: "M4 12h4l2-6 4 12 2-6h4" },
  {
    id: "settings",
    label: "Settings",
    icon: "M12 2l8 4v5c0 5-3.5 8-8 9-4.5-1-8-4-8-9V7z",
  },
];

export default function App() {
  const [page, setPage] = useState<Page>("swarm");
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [apiUnreachable, setApiUnreachable] = useState(false);
  const [counts, setCounts] = useState<{ bots: number; swarms: number } | null>(null);

  useEffect(() => {
    api
      .status()
      .then(setStatus)
      .catch(() => setApiUnreachable(true));
    Promise.all([api.listBots(), api.listSwarms()])
      .then(([bots, swarms]) => setCounts({ bots: bots.length, swarms: swarms.length }))
      .catch(() => {});
  }, []);

  return (
    <div className="grid h-screen grid-rows-[56px_1fr] pb-16 sm:pb-0 sm:grid-cols-[200px_1fr]">
      <header className="col-span-full flex items-center gap-5 border-b border-edge bg-void/80 px-5 backdrop-blur">
        <div className="flex items-center gap-2.5 font-display text-lg font-bold tracking-[0.14em]">
          <span className="inline-block h-5 w-2.5 bg-tron shadow-glow-sm" />
          NANOBOTS
        </div>
        <div className="ml-auto flex items-center gap-2 text-xs text-muted">
          {apiUnreachable ? (
            <>
              <StatusDot tone="danger" />
              nanobotd unreachable — run{" "}
              <code className="text-ink">nanobots up</code>
            </>
          ) : status ? (
            <>
              <StatusDot tone={status.oneclaw_configured ? "ok" : "warn"} />
              {status.oneclaw_configured
                ? "1Claw connected"
                : "Demo mode — no 1Claw key configured"}
            </>
          ) : (
            <>
              <StatusDot />
              connecting…
            </>
          )}
        </div>
      </header>

      <aside className="hidden border-r border-edge py-4 sm:flex sm:flex-col">
        <nav className="flex flex-col gap-0.5">
          {NAV.map((item) => (
            <button
              key={item.id}
              onClick={() => setPage(item.id)}
              className={`flex items-center gap-3 border-l-2 px-5 py-2.5 text-left text-sm transition-colors ${
                page === item.id
                  ? "border-tron bg-gradient-to-r from-tron/10 to-transparent text-ink"
                  : "border-transparent text-muted hover:text-ink"
              }`}
            >
              <svg
                viewBox="0 0 24 24"
                className="h-4 w-4 shrink-0"
                fill="none"
                stroke="currentColor"
                strokeWidth={1.6}
              >
                <path d={item.icon} />
              </svg>
              {item.label}
            </button>
          ))}
        </nav>
        <div className="mt-auto border-t border-edge px-5 pt-4 text-xs text-muted">
          <div className="font-display text-ink">Local stack</div>
          {counts ? `${counts.bots} bots, ${counts.swarms} swarms` : "…"}
        </div>
      </aside>

      <main className="min-h-0 min-w-0 overflow-hidden">
        {page === "swarm" && <SwarmsPage />}
        {page === "bots" && <BotLibrary />}
        {page === "runs" && <RunsPage />}
        {page === "settings" && <SettingsPage status={status} />}
      </main>

      <nav className="fixed inset-x-0 bottom-0 z-30 flex h-16 border-t border-edge bg-void/95 backdrop-blur sm:hidden">
        {NAV.map((item) => (
          <button
            key={item.id}
            onClick={() => setPage(item.id)}
            className={`flex flex-1 flex-col items-center justify-center gap-1 text-[11px] font-display ${
              page === item.id ? "text-tron" : "text-muted"
            }`}
          >
            <svg
              viewBox="0 0 24 24"
              className="h-[18px] w-[18px]"
              fill="none"
              stroke="currentColor"
              strokeWidth={1.6}
            >
              <path d={item.icon} />
            </svg>
            {item.label}
          </button>
        ))}
      </nav>
    </div>
  );
}

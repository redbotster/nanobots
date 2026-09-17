import { useEffect, useState } from "react";
import { listBotsCached } from "./lib/botsCache";
import { listSwarmsCached } from "./lib/swarmsCache";
import { SwarmsPage } from "./pages/SwarmsPage";
import { BotLibrary } from "./pages/BotLibrary";
import { RunsPage } from "./pages/RunsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { TeamPage } from "./pages/TeamPage";
import { LandingPage } from "./pages/LandingPage";
import { StatusDot } from "./components/StatusDot";
import { Switch } from "./components/Switch";
import { PrereqBanners } from "./components/PrereqBanners";
import { useUIMode } from "./lib/uiMode";
import { useEntered } from "./lib/entered";
import { useApprovalNotifications } from "./lib/useApprovalNotifications";
import { useTheme } from "./lib/theme";
import { useStatus } from "./lib/useStatus";

type Page = "swarm" | "bots" | "team" | "runs" | "settings";

const NAV: { id: Page; label: string; icon: string }[] = [
  { id: "bots", label: "Bot library", icon: "M4 7h16M4 12h10M4 17h7" },
  {
    id: "swarm",
    label: "Swarms",
    icon: "M4 5h7v6H4zM13 13h7v6h-7zM10 8h2a2 2 0 0 1 2 2v3",
  },
  // Between the catalog and the run log: the Team is about who works for
  // you, which sits naturally after "what exists" and before "what ran".
  { id: "team", label: "Team", icon: "M4 18h16M7 18V9m5 9V5m5 13v-6" },
  { id: "runs", label: "Runs", icon: "M4 12h4l2-6 4 12 2-6h4" },
  {
    id: "settings",
    label: "Settings",
    icon: "M12 2l8 4v5c0 5-3.5 8-8 9-4.5-1-8-4-8-9V7z",
  },
];

export default function App() {
  const [entered, setEntered] = useEntered();
  if (!entered) {
    return <LandingPage onEnter={() => setEntered(true)} />;
  }
  return <Dashboard onLeave={() => setEntered(false)} />;
}

function Dashboard({ onLeave }: { onLeave: () => void }) {
  const [page, setPage] = useState<Page>("swarm");
  const [uiMode, setUiMode] = useUIMode();
  const { status, unreachable: apiUnreachable } = useStatus();
  const [counts, setCounts] = useState<{ bots: number; swarms: number } | null>(null);
  const {
    count: pendingApprovals,
    permission: notifyPermission,
    requestPermission: enableNotifications,
  } = useApprovalNotifications();
  const { theme, resolved: resolvedTheme, setTheme } = useTheme();

  // Basic mode hides the bot library nav entry entirely — if a user was on
  // it and switches to basic, don't leave them on an orphaned page.
  useEffect(() => {
    if (uiMode === "basic" && page === "bots") setPage("swarm");
  }, [uiMode, page]);

  const visibleNav = uiMode === "basic" ? NAV.filter((item) => item.id !== "bots") : NAV;

  useEffect(() => {
    Promise.all([listBotsCached(), listSwarmsCached()])
      .then(([bots, { swarms }]) => setCounts({ bots: bots.length, swarms: swarms.length }))
      .catch(() => {});
  }, []);

  return (
    // min-w-0 matters: without it the implicit grid column sizes to the
    // header's min-content width, and a too-wide header drags the entire
    // shell into horizontal scroll on a phone (it did — 429px on a 390px
    // viewport) rather than the header itself adapting.
    <div className="grid h-screen min-w-0 grid-rows-[56px_1fr] pb-16 sm:pb-0 sm:grid-cols-[200px_1fr]">
      <header className="col-span-full flex min-w-0 items-center gap-3 border-b border-edge bg-void/80 px-4 backdrop-blur sm:gap-5 sm:px-5">
        <button
          onClick={onLeave}
          className="flex items-center gap-2.5 font-display text-lg font-bold tracking-[0.14em] text-ink"
          title="Back to landing page"
        >
          <span className="inline-block h-5 w-2.5 bg-tron shadow-glow-sm" />
          nanobots
        </button>
        {/* Everything here degrades to icon-only at phone widths — the dot
            alone still says "connected / demo / unreachable", and each
            control keeps a title for the full wording. */}
        <div className="ml-auto flex min-w-0 items-center gap-3 sm:gap-4">
          {notifyPermission === "default" && (
            <button
              onClick={enableNotifications}
              // Offset, because at this size a dotted underline sitting on the
              // baseline cuts through the descenders of "approval" and reads
              // as strikethrough — i.e. as a disabled control, which is the
              // opposite of what this is.
              className="shrink-0 text-xs text-muted underline decoration-dotted underline-offset-4 hover:text-ink"
              title="Get a browser notification the moment something needs your approval"
            >
              🔔<span className="hidden sm:inline"> Enable approval alerts</span>
            </button>
          )}
          {/* One button, one job: flip to the other theme. "System" is a
              real third state but it belongs in Settings — putting a
              three-way cycle up here would make the common action
              unpredictable. */}
          <button
            onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
            className="shrink-0 text-sm text-muted transition-colors hover:text-ink"
            title={
              theme === "system"
                ? `Following your system theme (${resolvedTheme}) — click for ${resolvedTheme === "dark" ? "light" : "dark"}`
                : `${resolvedTheme[0].toUpperCase()}${resolvedTheme.slice(1)} theme — click for ${resolvedTheme === "dark" ? "light" : "dark"}`
            }
            aria-label="Toggle light and dark theme"
          >
            {resolvedTheme === "dark" ? "☀" : "☾"}
          </button>
          <Switch
            checked={uiMode === "advanced"}
            onCheckedChange={(checked) => setUiMode(checked ? "advanced" : "basic")}
            label="Advanced"
            labelClassName="hidden sm:inline"
          />
          {/* The one fact worth a permanent slot: will a run do real work,
              or produce fixture text that looks exactly like real work?
              That is decided by whether a model is configured — which used
              to read "Demo mode — no 1Claw key configured" and was wrong
              the moment any provider key would do. 1Claw's own state lives
              in Settings, where there is room to explain it. */}
          <div
            className="flex min-w-0 items-center gap-2 text-xs text-muted"
            title={
              apiUnreachable
                ? "nanobotd unreachable — run `nanobots up`"
                : !status
                  ? "connecting…"
                  : status.llm_backend === "none"
                    ? "No model configured — every bot returns its demo fixtures. Set a key in Settings."
                    : status.llm_guardrails
                      ? `Live via ${status.llm_backend} — spend is budgeted and prompts are redacted before they leave this machine`
                      : `Live via ${status.llm_backend} — prompts go straight to the provider, with no budget ceiling or redaction`
            }
          >
            {apiUnreachable ? (
              <>
                <StatusDot tone="danger" />
                <span className="hidden truncate sm:inline">
                  nanobotd unreachable — run <code className="text-ink">nanobots up</code>
                </span>
              </>
            ) : status ? (
              <>
                <StatusDot
                  tone={
                    status.llm_backend === "none" ? "warn" : status.llm_guardrails ? "ok" : "muted"
                  }
                />
                <span className="hidden truncate sm:inline">
                  {status.llm_backend === "none"
                    ? "Demo mode — no model configured"
                    : `Live · ${status.llm_backend}`}
                </span>
              </>
            ) : (
              <>
                <StatusDot />
                <span className="hidden truncate sm:inline">connecting…</span>
              </>
            )}
          </div>
        </div>
      </header>

      <aside className="hidden border-r border-edge py-4 sm:flex sm:flex-col">
        <nav className="flex flex-col gap-0.5">
          {visibleNav.map((item) => (
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
              {item.id === "runs" && pendingApprovals > 0 && (
                <span className="ml-auto flex h-4 min-w-4 items-center justify-center rounded-full bg-warn px-1 text-[10px] font-bold text-void">
                  {pendingApprovals}
                </span>
              )}
            </button>
          ))}
        </nav>
        <div className="mt-auto border-t border-edge px-5 pt-4 text-xs text-muted">
          <div className="font-display text-ink">Local stack</div>
          {counts ? `${counts.bots} bots, ${counts.swarms} swarms` : "…"}
        </div>
      </aside>

      <main className="flex min-h-0 min-w-0 flex-col overflow-hidden">
        <PrereqBanners status={status} onOpenSettings={() => setPage("settings")} />
        <div className="min-h-0 flex-1 overflow-hidden">
          {page === "swarm" && (
            <SwarmsPage
              uiMode={uiMode}
              status={status}
              onOpenSettings={() => setPage("settings")}
            />
          )}
          {page === "bots" && <BotLibrary />}
          {page === "team" && <TeamPage />}
          {page === "runs" && <RunsPage onOpenSettings={() => setPage("settings")} />}
          {page === "settings" && <SettingsPage status={status} />}
        </div>
      </main>

      <nav className="fixed inset-x-0 bottom-0 z-30 flex h-16 border-t border-edge bg-void/95 backdrop-blur sm:hidden">
        {visibleNav.map((item) => (
          <button
            key={item.id}
            onClick={() => setPage(item.id)}
            className={`relative flex flex-1 flex-col items-center justify-center gap-1 text-[11px] font-display ${
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
            {item.id === "runs" && pendingApprovals > 0 && (
              <span className="absolute right-[22%] top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-warn px-1 text-[10px] font-bold text-void">
                {pendingApprovals}
              </span>
            )}
          </button>
        ))}
      </nav>
    </div>
  );
}

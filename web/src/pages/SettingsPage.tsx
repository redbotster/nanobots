import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { BotSummary, StatusResponse } from "../lib/types";
import { StatusDot } from "../components/StatusDot";

const CONNECTION_LABEL: Record<string, string> = {
  demo: "Demo data",
  oauth_1claw: "1Claw sign-in",
  oauth_native: "Sign-in",
  browser: "Browser Bridge",
  api_key_vault: "API key",
};

export function SettingsPage({ status }: { status: StatusResponse | null }) {
  const [bots, setBots] = useState<BotSummary[]>([]);

  useEffect(() => {
    api.listBots().then(setBots).catch(() => {});
  }, []);

  const services = new Map<string, { provider: string; connection: string; usedBy: string[] }>();
  for (const bot of bots) {
    for (const svc of bot.services) {
      const entry = services.get(svc.id) ?? {
        provider: svc.provider,
        connection: svc.connection ?? "oauth_1claw",
        usedBy: [],
      };
      entry.usedBy.push(bot.name);
      services.set(svc.id, entry);
    }
  }

  return (
    <div className="h-full overflow-auto p-6 sm:p-8">
      <h1 className="font-display text-xl font-medium text-ink">Settings</h1>

      <section className="mt-6 rounded-lg border border-edge-strong bg-panel p-5">
        <h2 className="font-display text-sm font-semibold text-ink">1Claw</h2>
        <div className="mt-3 flex items-center gap-2 text-sm">
          <StatusDot tone={status?.oneclaw_configured ? "ok" : "warn"} />
          {status?.oneclaw_configured
            ? "Connected — bots run live wherever their services allow it"
            : "No key configured — everything runs in demo mode"}
        </div>
        {!status?.oneclaw_configured && (
          <p className="mt-3 text-[13px] leading-relaxed text-muted">
            Drop your 1Claw Human API key at{" "}
            <code className="text-ink">~/.secrets/nanobots.env</code> as{" "}
            <code className="text-ink">ONECLAW_API_KEY=...</code> and restart{" "}
            <code className="text-ink">nanobots up</code>. It's read once at
            startup, never written into this repo, and never shown to a bot —
            see the README.
          </p>
        )}
      </section>

      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-5">
        <h2 className="font-display text-sm font-semibold text-ink">Services</h2>
        <p className="mt-1 text-[13px] text-muted">
          How each bot's declared services get connected — every service
          resolves to one of a few standard strategies, never a raw API key
          in a bot's hands.
        </p>
        <div className="mt-4 flex flex-col divide-y divide-edge">
          {[...services.entries()].map(([id, s]) => (
            <div key={id} className="flex items-center gap-3 py-3">
              <StatusDot tone={s.connection === "demo" ? "warn" : "ok"} />
              <div className="min-w-0 flex-1">
                <div className="text-sm text-ink">
                  {id} <span className="text-muted">· {s.provider}</span>
                </div>
                <div className="text-[11px] text-muted">
                  used by {s.usedBy.join(", ")}
                </div>
              </div>
              <div className="rounded border border-edge px-2 py-1 text-[11px] text-ink">
                {CONNECTION_LABEL[s.connection] ?? s.connection}
              </div>
            </div>
          ))}
        </div>
        <p className="mt-4 text-[12px] text-muted">
          Gmail and Drive are on demo data right now — Google blocks
          automated-browser sign-in outright, so real access needs either
          1Claw's own Google OAuth provider to add Gmail scopes, or a
          dedicated Google sign-in client. See the README for the full story.
        </p>
      </section>
    </div>
  );
}

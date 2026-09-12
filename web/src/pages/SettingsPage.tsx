import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { BotSummary, ConnectableService, ConnectionStatus, StatusResponse } from "../lib/types";
import { StatusDot } from "../components/StatusDot";
import { AppearanceSection } from "../components/settings/AppearanceSection";
import { OneClawKeySetup } from "../components/settings/OneClawKeySetup";
import { OAuthConnectRow, TokenConnectRow } from "../components/settings/ConnectRows";

const CONNECTION_LABEL: Record<string, string> = {
  demo: "Demo data",
  oauth_1claw: "1Claw sign-in",
  oauth_native: "Sign-in",
  browser: "Browser Bridge",
  api_key_vault: "API key",
};

export function SettingsPage({ status }: { status: StatusResponse | null }) {
  const [bots, setBots] = useState<BotSummary[]>([]);
  const [connections, setConnections] = useState<ConnectionStatus[] | null>(null);
  const [connectionsError, setConnectionsError] = useState<string | null>(null);

  const reloadConnections = () =>
    api
      .listConnections()
      .then(setConnections)
      .catch((e) => setConnectionsError(String(e)));

  useEffect(() => {
    api.listBots().then(setBots).catch(() => {});
    reloadConnections();
  }, []);

  const isConnected = (service: ConnectableService) =>
    connections?.find((c) => c.service === service)?.connected ?? false;

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
    <div className="h-full overflow-auto p-5 sm:p-6">
      <h1 className="font-display text-xl font-medium text-ink">Settings</h1>

      <AppearanceSection />

      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">1Claw</h2>
        <div className="mt-3 flex items-center gap-2 text-sm">
          <StatusDot tone={status?.oneclaw_configured ? "ok" : "warn"} />
          {status?.oneclaw_configured
            ? "Connected — bots run live wherever their services allow it"
            : "No key configured — everything runs in demo mode"}
        </div>
        {!status?.oneclaw_configured && <OneClawKeySetup />}
        {status?.oneclaw_configured && (
          <div className="mt-2 flex items-center gap-2 text-sm">
            <StatusDot tone={status.vault_locked ? "warn" : "ok"} />
            {status.vault_locked
              ? `Vault locked — ${status.vault_reason || "unlock it with your passkey"}`
              : "Vault unlocked — connected credentials are readable"}
          </div>
        )}
      </section>

      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">Memory</h2>
        <div className="mt-3 flex items-center gap-2 text-sm">
          <StatusDot tone={status?.memory_recall ? "ok" : "muted"} />
          {status
            ? status.memory_recall
              ? `${status.memory_backend} — bots can remember and be asked about it`
              : `${status.memory_backend} — stores values by key`
            : "Checking…"}
        </div>
        {status && !status.memory_recall && (
          <p className="mt-1.5 text-[12px] leading-snug text-muted">
            Bots that could learn from what happened before are running
            without that. inbox-triage, for one, falls back to its static
            rules instead of what you've actually treated as urgent. Set{" "}
            <code className="text-ink">NANOBOTS_MEMORY=honcho</code> with a{" "}
            <code className="text-ink">HONCHO_URL</code> to change it — see
            docs/memory.md.
          </p>
        )}
      </section>

      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">Docker</h2>
        <div className="mt-3 flex items-center gap-2 text-sm">
          <StatusDot tone={status?.docker_available ? "ok" : "warn"} />
          {status?.docker_available
            ? "Running — every bot gets its own container"
            : `${status?.docker_reason ?? "Checking…"} — bots run in containers, so nothing runs until it's up`}
        </div>
      </section>

      {status?.oneclaw_configured && (
        <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
          <h2 className="font-display text-sm font-semibold text-ink">Connect a service</h2>
          <p className="mt-1 text-[13px] text-muted">
            Every credential goes straight into your 1Claw vault — never onto
            this machine's disk, never into a bot container.
          </p>
          {connectionsError && (
            <p className="mt-3 text-[13px] text-danger">{connectionsError}</p>
          )}
          <div className="mt-4 flex flex-col divide-y divide-edge">
            <OAuthConnectRow
              provider="google"
              label="Google"
              hint="Gmail, Drive, Sheets, Calendar — opens your browser to sign in"
              connected={isConnected("google")}
              onConnected={reloadConnections}
              connectFn={api.connectGoogleStart}
            />
            <OAuthConnectRow
              provider="x"
              label="X"
              hint="Posting to X — opens your browser to sign in"
              connected={isConnected("x")}
              onConnected={reloadConnections}
              connectFn={api.connectXStart}
            />
            <OAuthConnectRow
              provider="linkedin"
              label="LinkedIn"
              hint="Posting to LinkedIn — opens your browser to sign in"
              connected={isConnected("linkedin")}
              onConnected={reloadConnections}
              connectFn={api.connectLinkedInStart}
            />
            <TokenConnectRow
              service="slack"
              label="Slack"
              hint="A bot token (xoxb-...) from api.slack.com/apps, scoped to chat:write."
              connected={isConnected("slack")}
              onConnected={reloadConnections}
            />
            <TokenConnectRow
              service="github"
              label="GitHub"
              hint="A personal access token, scoped to repo (or public_repo for public repos only)."
              connected={isConnected("github")}
              onConnected={reloadConnections}
            />
            <TokenConnectRow
              service="stripe"
              label="Stripe"
              hint="A secret key from dashboard.stripe.com/apikeys."
              connected={isConnected("stripe")}
              onConnected={reloadConnections}
            />
            <TokenConnectRow
              service="hubspot"
              label="HubSpot"
              hint="A private app token, scoped to crm.objects.contacts.read/.write."
              connected={isConnected("hubspot")}
              onConnected={reloadConnections}
            />
          </div>
        </section>
      )}

      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">Services in use</h2>
        <p className="mt-1 text-[13px] text-muted">
          How each bot's declared services get connected — every service
          resolves to one of a few standard strategies, never a raw API key
          in a bot's hands.
        </p>
        <div className="mt-4 flex flex-col divide-y divide-edge">
          {[...services.entries()].map(([id, s]) => (
            <div key={id} className="flex items-center gap-3 py-2.5">
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
          Every bot ships on demo data by default, even after you connect a
          service above — switch a specific bot's <code>connection:</code> in
          its <code>nanobot.yaml</code> (or the visual builder, soon) to use
          it for real. See the README for the full story on why Gmail/Drive
          specifically needed a dedicated Google sign-in rather than 1Claw's
          own OAuth registry.
        </p>
      </section>
    </div>
  );
}

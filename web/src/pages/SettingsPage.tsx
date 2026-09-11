import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { BotSummary, ConnectableService, ConnectionStatus, StatusResponse } from "../lib/types";
import { StatusDot } from "../components/StatusDot";
import { Button } from "../components/Button";

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

      <section className="mt-5 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">1Claw</h2>
        <div className="mt-3 flex items-center gap-2 text-sm">
          <StatusDot tone={status?.oneclaw_configured ? "ok" : "warn"} />
          {status?.oneclaw_configured
            ? "Connected — bots run live wherever their services allow it"
            : "No key configured — everything runs in demo mode"}
        </div>
        {!status?.oneclaw_configured && <OneClawKeySetup />}
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
              label="Google"
              hint="Gmail, Drive, Sheets, Calendar — opens your browser to sign in"
              connected={isConnected("google")}
              onConnected={reloadConnections}
              connectFn={api.connectGoogleStart}
            />
            <OAuthConnectRow
              label="X"
              hint="Posting to X — opens your browser to sign in"
              connected={isConnected("x")}
              onConnected={reloadConnections}
              connectFn={api.connectXStart}
            />
            <OAuthConnectRow
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

// OAuthConnectRow is the shared shape for every service that connects via a
// real interactive OAuth round trip (Google, X, LinkedIn) rather than a
// pasted static token — the request blocks while the human approves in
// their browser, so this shows a "waiting" state for that whole time.
/** The one credential everything else depends on previously had no in-app
 * setup path at all — every downstream service got a one-click Connect
 * story, while this, the most foundational one, told a human to go open a
 * text editor. Paste it, we validate it actually authenticates and save it
 * for you (internal/api/setup.go); still needs a restart to take effect,
 * same contract this build already has for GOOGLE_OAUTH_CLIENT_ID and
 * friends, rather than threading a hot-reloadable client through every
 * handler that holds one today. */
function OneClawKeySetup() {
  const [key, setKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const submit = async () => {
    if (!key.trim()) return;
    setSaving(true);
    setError(null);
    try {
      await api.setupOneClawKey(key.trim());
      setSaved(true);
      setKey("");
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  };

  if (saved) {
    return (
      <div className="mt-3 rounded border border-ok/40 bg-ok/5 px-3 py-2.5 text-[13px] text-ink">
        Saved. Restart <code className="text-ink">nanobots up</code> to pick it up.
      </div>
    );
  }

  return (
    <div className="mt-3">
      <p className="text-[13px] leading-relaxed text-muted">
        Paste your 1Claw Human API key — we'll check it works, then save it
        to <code className="text-ink">~/.secrets/nanobots.env</code> for
        you. It's never written into this repo or shown to a bot.
      </p>
      <div className="mt-2 flex items-center gap-2">
        <input
          type="password"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
          placeholder="1ck_..."
          disabled={saving}
          className="flex-1 rounded border border-edge-strong bg-void px-2.5 py-1.5 text-xs text-ink placeholder:text-muted focus:border-tron focus:outline-none disabled:opacity-60"
        />
        <Button variant="primary" onClick={submit} disabled={saving || !key.trim()}>
          {saving ? "Checking…" : "Save"}
        </Button>
      </div>
      {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
    </div>
  );
}

function OAuthConnectRow({
  label,
  hint,
  connected,
  onConnected,
  connectFn,
}: {
  label: string;
  hint: string;
  connected: boolean;
  onConnected: () => void;
  connectFn: () => Promise<ConnectionStatus>;
}) {
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const connect = async () => {
    setConnecting(true);
    setError(null);
    try {
      await connectFn();
      onConnected();
    } catch (e) {
      setError(String(e));
    } finally {
      setConnecting(false);
    }
  };

  return (
    <div className="py-2.5">
      <div className="flex items-center gap-3">
        <StatusDot tone={connected ? "ok" : "muted"} />
        <div className="min-w-0 flex-1">
          <div className="text-sm text-ink">{label}</div>
          <div className="text-[11px] text-muted">{hint}</div>
        </div>
        <Button variant="ghost" onClick={connect} disabled={connecting}>
          {connecting ? "Waiting for you to approve…" : connected ? "Reconnect" : "Connect"}
        </Button>
      </div>
      {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
    </div>
  );
}

function TokenConnectRow({
  service,
  label,
  hint,
  connected,
  onConnected,
}: {
  service: ConnectableService;
  label: string;
  hint: string;
  connected: boolean;
  onConnected: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (!token.trim()) return;
    setSaving(true);
    setError(null);
    try {
      await api.connectToken(service, token.trim());
      setToken("");
      setOpen(false);
      onConnected();
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="py-2.5">
      <div className="flex items-center gap-3">
        <StatusDot tone={connected ? "ok" : "muted"} />
        <div className="min-w-0 flex-1">
          <div className="text-sm text-ink">{label}</div>
          <div className="text-[11px] text-muted">{hint}</div>
        </div>
        <Button variant="ghost" onClick={() => setOpen((o) => !o)}>
          {connected ? "Reconnect" : "Connect"}
        </Button>
      </div>
      {open && (
        <div className="mt-3 flex items-center gap-2">
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
            placeholder="Paste token…"
            autoFocus
            className="flex-1 rounded border border-edge-strong bg-void px-2.5 py-1.5 text-xs text-ink placeholder:text-muted focus:border-tron focus:outline-none"
          />
          <Button variant="primary" onClick={submit} disabled={saving || !token.trim()}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
      )}
      {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
    </div>
  );
}

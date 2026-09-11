import { useState } from "react";
import type { BotSummary, ConnectableService, ConnectionStatus, Service } from "../lib/types";
import { PortBadge } from "./PortBadge";
import { Switch } from "./Switch";
import { Button } from "./Button";
import { api } from "../lib/api";
import { isOAuthProvider, startOAuthConnect } from "../lib/connectProvider";

const CONNECTABLE = new Set<string>([
  "google",
  "slack",
  "github",
  "stripe",
  "hubspot",
  "x",
  "linkedin",
]);

/** One service's demo/live switch — connecting the account (if needed) and
 * flipping this bot's own connection: are one action from here, not two
 * trips through Settings and a text editor. An OAuth provider connects in
 * one click (opens the browser); a token provider expands a small inline
 * paste field first — same shape as Settings' own TokenConnectRow, so
 * connecting from either place looks and feels identical, no
 * window.prompt. */
function ServiceToggle({
  botId,
  service,
  connected,
  onChanged,
}: {
  botId: string;
  service: Service;
  connected: boolean;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showTokenInput, setShowTokenInput] = useState(false);
  const [token, setToken] = useState("");
  const isLive = (service.connection ?? "demo") !== "demo";
  const provider = service.provider as ConnectableService;

  const goLive = async () => {
    setBusy(true);
    setError(null);
    try {
      if (!connected) {
        await startOAuthConnect(provider);
      }
      await api.setBotServiceConnection(botId, service.id, true);
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const submitToken = async () => {
    if (!token.trim()) return;
    setBusy(true);
    setError(null);
    try {
      await api.connectToken(provider, token.trim());
      await api.setBotServiceConnection(botId, service.id, true);
      setShowTokenInput(false);
      setToken("");
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const goDemo = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.setBotServiceConnection(botId, service.id, false);
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const onCheckedChange = (checked: boolean) => {
    if (!checked) return void goDemo();
    if (!connected && !isOAuthProvider(provider)) {
      setShowTokenInput(true);
      return;
    }
    void goLive();
  };

  return (
    <div>
      <div className="flex items-center gap-1.5">
        <Switch checked={isLive} onCheckedChange={onCheckedChange} />
        <span className="text-[11px] text-ink">{service.id}</span>
        {busy && <span className="text-[10px] text-muted">…</span>}
        {!busy && !isLive && !connected && !showTokenInput && (
          <span className="text-[10px] text-muted">(connect &amp; go live)</span>
        )}
        {error && <span className="text-[10px] text-danger" title={error}>failed</span>}
      </div>
      {showTokenInput && (
        <div className="mt-1.5 flex items-center gap-1.5">
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submitToken()}
            placeholder={`Paste your ${provider} token…`}
            autoFocus
            disabled={busy}
            className="min-w-0 flex-1 rounded border border-edge-strong bg-void px-2 py-1 text-[11px] text-ink placeholder:text-muted focus:border-tron focus:outline-none disabled:opacity-60"
          />
          <Button variant="primary" className="px-2 py-1 text-[11px]" onClick={submitToken} disabled={busy || !token.trim()}>
            Connect
          </Button>
          <Button variant="ghost" className="px-2 py-1 text-[11px]" onClick={() => setShowTokenInput(false)} disabled={busy}>
            Cancel
          </Button>
        </div>
      )}
    </div>
  );
}

/** The bot-library card shell. Interactive mode (BotLibrary) adds a
 * demo/live switch per connectable service; non-interactive mode (the
 * foundry's "review this new bot" screen, for a bot that isn't even in the
 * catalog yet) falls back to the original static badges. */
export function BotCard({
  bot,
  connections,
  onChanged,
}: {
  bot: BotSummary;
  connections?: ConnectionStatus[];
  onChanged?: () => void;
}) {
  const interactive = connections !== undefined && onChanged !== undefined;

  return (
    <div className="fade-in rounded-lg border border-edge-strong bg-panel p-4 transition-shadow hover:shadow-glow-sm">
      <div className="flex items-center justify-between font-display text-[11px] tracking-wide text-tron">
        {bot.id} <span className="text-muted">v{bot.version}</span>
      </div>
      <h2 className="mt-1 font-display text-base font-semibold text-ink">
        {bot.name}
      </h2>
      <p className="mt-1 text-[13px] leading-snug text-muted">
        {bot.description}
      </p>

      <div className="mt-3 flex flex-col gap-1.5">
        {(bot.services ?? []).map((s) =>
          interactive && CONNECTABLE.has(s.provider) ? (
            <ServiceToggle
              key={s.id}
              botId={bot.id}
              service={s}
              connected={connections!.find((c) => c.service === s.provider)?.connected ?? false}
              onChanged={onChanged!}
            />
          ) : (
            <span
              key={s.id}
              className="inline-block w-fit rounded border border-edge px-2 py-0.5 text-[11px] text-ink"
            >
              {s.id}
              {(s.connection ?? "demo") === "demo" && (
                <span className="text-warn"> · demo</span>
              )}
            </span>
          ),
        )}
        <span className="w-fit rounded border border-edge px-2 py-0.5 text-[11px] text-muted">
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
  );
}

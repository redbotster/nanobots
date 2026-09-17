import { useCallback, useEffect, useState } from "react";
import { api } from "../../lib/api";
import type { ConnectableService, ConnectionStatus } from "../../lib/types";
import { StatusDot } from "../../components/StatusDot";
import { Button } from "../../components/Button";

/**
 * The second half of connecting an account, which used to have no bulk path
 * at all.
 *
 * Every bot ships on `connection: demo` — the right default, since nothing
 * should read a real inbox until a human says so. But 24 of the catalog's
 * 32 declared services are Google, so "connect Google and start using it"
 * meant one OAuth round trip followed by twenty-four separate toggles
 * hunted down across the bot library. On a fresh install all 14 swarms sit
 * entirely on demo data, and nothing in the UI offered a way out of that
 * except one bot at a time.
 *
 * Both directions are offered, and going back is never gated: undoing
 * should always be at least as easy as doing.
 */
function UseInBots({ provider, connected }: { provider: string; connected: boolean }) {
  const [counts, setCounts] = useState<{ demo: number; live: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(() => {
    api
      .providerBots(provider)
      .then((r) => setCounts({ demo: r.demo.length, live: r.live.length }))
      .catch(() => setCounts(null));
  }, [provider]);

  useEffect(refresh, [refresh, connected]);

  const apply = async (live: boolean) => {
    setBusy(true);
    setError(null);
    setNote(null);
    try {
      const r = await api.setProviderConnection(provider, live);
      const failed = Object.keys(r.failed ?? {}).length;
      setNote(
        `${r.changed.length} bot${r.changed.length === 1 ? "" : "s"} now ${live ? "use your account" : "back on demo data"}` +
          (failed ? ` — ${failed} couldn't be changed` : ""),
      );
      refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  if (!counts || (counts.demo === 0 && counts.live === 0)) return null;

  return (
    <div className="mt-2 flex flex-wrap items-center gap-2 pl-5 text-[12px] text-muted">
      {connected && counts.demo > 0 && (
        <>
          <span>
            {counts.demo} bot{counts.demo === 1 ? "" : "s"} still on demo data
          </span>
          <button
            onClick={() => void apply(true)}
            disabled={busy}
            className="rounded border border-edge-strong px-2 py-0.5 text-ink transition-colors hover:border-tron disabled:opacity-50"
          >
            {busy
              ? "Switching…"
              : `Use my account in ${counts.demo === 1 ? "it" : "all " + counts.demo}`}
          </button>
        </>
      )}
      {counts.live > 0 && (
        <>
          <span className="text-ok">
            {counts.live} bot{counts.live === 1 ? "" : "s"} using your account
          </span>
          <button
            onClick={() => void apply(false)}
            disabled={busy}
            className="rounded border border-edge px-2 py-0.5 transition-colors hover:border-warn hover:text-warn disabled:opacity-50"
          >
            Back to demo
          </button>
        </>
      )}
      {!connected && counts.demo > 0 && (
        <span>
          {counts.demo} bot{counts.demo === 1 ? "" : "s"} would use this once connected
        </span>
      )}
      {note && <span className="w-full text-ok">{note}</span>}
      {error && <span className="w-full text-danger">{error}</span>}
    </div>
  );
}

export function OAuthConnectRow({
  provider,
  label,
  hint,
  connected,
  onConnected,
  connectFn,
}: {
  provider: string;
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
      <UseInBots provider={provider} connected={connected} />
    </div>
  );
}

export function TokenConnectRow({
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
      <UseInBots provider={service} connected={connected} />
    </div>
  );
}

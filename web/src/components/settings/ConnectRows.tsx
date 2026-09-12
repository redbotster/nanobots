import { useState } from "react";
import { api } from "../../lib/api";
import type { ConnectableService, ConnectionStatus } from "../../lib/types";
import { StatusDot } from "../../components/StatusDot";
import { Button } from "../../components/Button";


export function OAuthConnectRow({
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
    </div>
  );
}

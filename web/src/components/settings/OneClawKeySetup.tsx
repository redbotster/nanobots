import { useState } from "react";
import { api } from "../../lib/api";
import { Button } from "../../components/Button";

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
export function OneClawKeySetup() {
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
        Paste your 1Claw Human API key — we'll check it works, then save it to{" "}
        <code className="text-ink">~/.secrets/nanobots.env</code> for you. It's never written into
        this repo or shown to a bot.
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

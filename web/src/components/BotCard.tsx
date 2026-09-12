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
/** The bot's own default behaviour, editable in place.
 *
 * Every LLM bot ships a suggested `instructions` value written for its job
 * — "Anything mentioning data loss or billing is top priority". It was only
 * reachable by opening the bot's nanobot.yaml, or by overriding it inside
 * one swarm in the builder's inspector. This is the catalog-wide edit: what
 * the bot does by default, everywhere it's used.
 *
 * Read-only until the card is given onChanged, so the foundry's review
 * preview stays a preview. */
function InstructionsEditor({
  botId,
  current,
  onChanged,
}: {
  botId: string;
  current: string;
  onChanged: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState(current);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setBotInstructions(botId, text);
      setOpen(false);
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  };

  if (!open) {
    return (
      <button
        onClick={() => {
          setText(current);
          setOpen(true);
        }}
        className="mt-3 w-full rounded border border-edge px-2.5 py-1.5 text-left text-[11px] text-muted transition-colors hover:border-tron hover:text-ink"
        title="How this bot works by default, everywhere it's used"
      >
        <span className="text-muted/70">how it works · </span>
        {current || <span className="italic">no default set — click to add one</span>}
      </button>
    );
  }

  return (
    <div className="mt-3 rounded border border-edge-strong p-2">
      <label className="block text-[10px] uppercase tracking-wider text-muted">
        How this bot should work
      </label>
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value.replace(/\n/g, " "))}
        rows={3}
        maxLength={2000}
        autoFocus
        placeholder="Tone, priorities, wording…"
        className="mt-1 w-full resize-none rounded border border-edge bg-void px-2 py-1.5 text-[12px] text-ink placeholder:text-muted focus:border-tron focus:outline-none"
      />
      <p className="mt-1 text-[10px] leading-snug text-muted">
        Shapes how it does its job. It can't change what the bot produces, or
        any rule about sending, publishing or paying.
      </p>
      {error && <p className="mt-1 text-[11px] text-danger">{error}</p>}
      <div className="mt-1.5 flex gap-1.5">
        <button
          onClick={() => void save()}
          disabled={saving}
          className="rounded border border-edge-strong px-2 py-0.5 text-[11px] text-ink hover:border-tron disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save"}
        </button>
        <button
          onClick={() => setOpen(false)}
          className="rounded border border-edge px-2 py-0.5 text-[11px] text-muted hover:text-ink"
        >
          Cancel
        </button>
      </div>
    </div>
  );
}

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
  // `instructions` is shown as its own control rather than as one more port
  // badge — it's the one input a person is expected to set by hand.
  const instructionsPort = (bot.inputs ?? []).find((p) => p.name === "instructions");

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

      {interactive && instructionsPort && (
        <InstructionsEditor
          botId={bot.id}
          current={instructionsPort.default ?? ""}
          onChanged={onChanged!}
        />
      )}

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

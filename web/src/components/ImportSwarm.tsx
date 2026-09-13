import { useState } from "react";
import type { ImportResult } from "../lib/types";
import { api } from "../lib/api";
import { Button } from "./Button";

/** Taking in a swarm somebody sent you.
 *
 * The counterpart to Share. `nanobots import` has existed and been the only
 * way, which makes "here, try my swarm" a thing you can only accept from a
 * terminal.
 *
 * Two steps, deliberately. A swarm is executable — `get-paid` emails your
 * customers — and the bundle format exists precisely so that what it will
 * do to the outside world is legible before it runs. Pasting is step one
 * and shows what arrived; only Add it writes anything. A one-click import
 * that saves first and warns afterwards would have thrown that away.
 */
export function ImportSwarm({ onImported }: { onImported: () => void }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [preview, setPreview] = useState<ImportResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const run = async (confirm: boolean) => {
    setBusy(true);
    setError(null);
    try {
      const res = await api.importSwarm(text, confirm);
      if (res.imported) {
        setOpen(false);
        setText("");
        setPreview(null);
        onImported();
        return;
      }
      setPreview(res);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  if (!open) {
    return (
      <Button variant="ghost" onClick={() => setOpen(true)}>
        Add a shared swarm
      </Button>
    );
  }

  return (
    <div className="mt-3 w-full rounded-lg border border-edge bg-panel/40 p-3">
      <p className="text-[12px] text-muted">
        Paste a swarm bundle — the file someone got from <span className="text-ink">Share</span>.
      </p>
      <textarea
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setPreview(null);
        }}
        rows={5}
        spellCheck={false}
        placeholder="# A nanobots swarm bundle. …"
        className="mt-2 w-full rounded bg-void px-2.5 py-2 font-mono text-[11px] text-ink outline-none ring-1 ring-edge focus:ring-tron"
      />

      {error && <p className="mt-2 text-[11px] text-danger">{error}</p>}

      {preview && (
        <div className="mt-2 space-y-1.5 rounded border border-edge bg-void px-2.5 py-2 text-[12px]">
          <div className="text-ink">{preview.name}</div>
          {preview.missing?.length ? (
            // Nothing is written when a bot is missing, confirmed or not —
            // a swarm that looks saved and fails at run time is the worse
            // order. Say which bots, so it is actionable.
            <p className="text-danger">
              You don't have {preview.missing.join(", ")} — nothing was added.
            </p>
          ) : (
            <>
              {preview.acts?.length ? (
                <p className="text-warn">
                  ⚠ When it runs it can write to: {preview.acts.join(", ")}. Read it
                  before running it, the same as any script someone sends you.
                </p>
              ) : (
                <p className="text-muted">It doesn't write anywhere when it runs.</p>
              )}
              {preview.connects?.length ? (
                <p className="text-muted">
                  It wants these accounts connected: {preview.connects.join(", ")}
                </p>
              ) : null}
            </>
          )}
        </div>
      )}

      <div className="mt-2.5 flex items-center gap-2">
        {preview && !preview.missing?.length ? (
          <Button variant="primary" onClick={() => run(true)} disabled={busy}>
            {busy ? "Adding…" : "Add it"}
          </Button>
        ) : (
          <Button variant="primary" onClick={() => run(false)} disabled={busy || !text.trim()}>
            {busy ? "Reading…" : "Read it"}
          </Button>
        )}
        <Button
          variant="ghost"
          onClick={() => {
            setOpen(false);
            setPreview(null);
            setError(null);
          }}
        >
          Cancel
        </Button>
      </div>
    </div>
  );
}

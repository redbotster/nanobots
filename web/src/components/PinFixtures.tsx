import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import type { FixturePreview, RunFixtures } from "../lib/types";

/**
 * Turn what a run actually got back into the bots' test data.
 *
 * Every bot ships fixtures so `nanobots conform` and `go test` can run it
 * offline, and those fixtures are hand-written — somebody's guess at what a
 * model or an API returns. Guesses drift: a fixture written before a prompt
 * changed still passes while the real bot has been broken for weeks.
 *
 * Deliberately a two-step. This shows exactly what would be written and
 * what it would replace; the button does it. Writing into `bots/` is a real
 * change to the repo that belongs in a commit, and a control that silently
 * rewrote test data would be the worst possible way to discover a fixture
 * had changed.
 */
function FileRow({
  preview,
  checked,
  onToggle,
}: {
  preview: FixturePreview;
  checked: boolean;
  onToggle: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-t border-edge/60 py-1.5 first:border-t-0">
      <label className="flex cursor-pointer items-center gap-2 text-[12px]">
        <input type="checkbox" checked={checked} onChange={onToggle} className="accent-tron" />
        <code className="text-ink">{preview.file}</code>
        <span
          className={
            preview.status === "replaces" ? "text-[10px] text-warn" : "text-[10px] text-muted"
          }
        >
          {preview.status === "replaces" ? "replaces what's committed" : "new"}
        </span>
        <button
          type="button"
          onClick={(e) => {
            e.preventDefault();
            setOpen((v) => !v);
          }}
          className="ml-auto text-[11px] text-muted underline decoration-dotted hover:text-ink"
        >
          {open ? "hide" : "look"}
        </button>
      </label>
      {open && (
        <div className="mt-1.5 grid gap-1.5 sm:grid-cols-2">
          {preview.current && (
            <div>
              <div className="text-[10px] uppercase tracking-wider text-muted">on disk now</div>
              <pre className="mt-0.5 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-void px-2 py-1.5 text-[11px] text-muted">
                {preview.current}
              </pre>
            </div>
          )}
          <div>
            <div className="text-[10px] uppercase tracking-wider text-muted">from this run</div>
            <pre className="mt-0.5 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-void px-2 py-1.5 text-[11px] text-ink">
              {preview.content}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}

export function PinFixtures({ runId }: { runId: string }) {
  const [fx, setFx] = useState<RunFixtures | null>(null);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [written, setWritten] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .runFixtures(runId)
      .then((r) => {
        setFx(r);
        const next: Record<string, boolean> = {};
        for (const [bot, files] of Object.entries(r.bots)) {
          // Default to everything new, nothing that overwrites: the safe
          // half is the one you can take without reading.
          for (const f of files) next[`${bot}/${f.file}`] = f.status === "new";
        }
        setSelected(next);
      })
      .catch((e) => setError(String(e)));
  }, [runId]);
  useEffect(load, [load]);

  const entries = Object.entries(fx?.bots ?? {});
  if (error) return null;
  if (entries.length === 0) return null;

  const chosen = Object.entries(selected)
    .filter(([, v]) => v)
    .map(([k]) => k);

  const pin = async () => {
    setBusy(true);
    setError(null);
    try {
      const all: string[] = [];
      for (const [bot, files] of entries) {
        const want = files.map((f) => f.file).filter((f) => selected[`${bot}/${f}`]);
        if (want.length === 0) continue;
        const res = await api.pinFixtures(runId, { bot, files: want });
        all.push(...res.written);
      }
      setWritten(all);
      load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-3 rounded-lg border border-edge-strong bg-panel/50 px-3.5 py-2.5">
      <span className="font-display text-[11px] uppercase tracking-wider text-muted">
        Keep this run as test data
      </span>
      <p className="mt-1 text-[12px] leading-snug text-muted">
        These bots' fixtures are what <code className="text-ink">nanobots conform</code> and the
        test suite replay offline. They're hand-written today — this is what actually came back.
      </p>

      {entries.map(([bot, files]) => (
        <div key={bot} className="mt-2">
          <span className="font-display text-[12px] text-ink">{bot}</span>
          {files.map((f) => (
            <FileRow
              key={f.file}
              preview={f}
              checked={!!selected[`${bot}/${f.file}`]}
              onToggle={() =>
                setSelected((s) => ({ ...s, [`${bot}/${f.file}`]: !s[`${bot}/${f.file}`] }))
              }
            />
          ))}
        </div>
      ))}

      <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px]">
        <button
          onClick={pin}
          disabled={busy || chosen.length === 0}
          className="rounded border border-edge-strong px-2 py-0.5 text-ink transition-colors hover:border-tron disabled:opacity-40"
        >
          {busy ? "Writing…" : `Write ${chosen.length} file${chosen.length === 1 ? "" : "s"}`}
        </button>
        <span className="text-muted/70">
          writes into <code>bots/…/fixtures/</code> — review the diff before committing
        </span>
        {written && written.length > 0 && (
          <span className="text-ok">wrote {written.join(", ")}</span>
        )}
      </div>
    </div>
  );
}

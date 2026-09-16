import { useEffect, useState } from "react";
import { api } from "../../lib/api";

/** When a new swarm should run on its own.
 *
 * The composer can now answer this — ask for "every friday summarise my
 * overdue invoices" and it writes `0 9 * * 5` — but a cron expression the
 * user never sees is a promise they cannot check. Before this, a composed
 * swarm was always manual, and its own description cheerfully claimed
 * otherwise.
 *
 * Presets first, because "every weekday morning" is what people actually
 * mean and nobody should have to know cron to say it. The raw field is
 * there underneath for anyone who does.
 *
 * Only shown when creating. Editing an existing swarm leaves its trigger
 * alone — the builder does not model triggers, and offering a control that
 * silently did nothing would be worse than offering none.
 */
const PRESETS: { label: string; expr: string }[] = [
  { label: "Manual only", expr: "" },
  { label: "Every weekday, 7am", expr: "0 7 * * 1-5" },
  { label: "Every weekday, 5pm", expr: "0 17 * * 1-5" },
  { label: "Mondays, 9am", expr: "0 9 * * 1" },
  { label: "Fridays, 3pm", expr: "0 15 * * 5" },
  { label: "Every hour", expr: "0 * * * *" },
  { label: "Every 30 minutes", expr: "*/30 * * * *" },
];

export function SchedulePicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (expr: string) => void;
}) {
  // Open automatically when the composer already picked one, so a schedule
  // that was chosen for you is never hidden behind a click.
  const [custom, setCustom] = useState(value !== "" && !PRESETS.some((p) => p.expr === value));
  // Described by the server, using the same parser that decides whether it
  // fires — so what this says and what actually happens cannot drift. It
  // also validates as you type: a cron the scheduler cannot read says so
  // here rather than at save time.
  const [described, setDescribed] = useState<{ ok: boolean; text: string } | null>(null);
  useEffect(() => {
    if (!custom || value.trim() === "") {
      setDescribed(null);
      return;
    }
    let live = true;
    const id = setTimeout(() => {
      api
        .describeSchedule(value)
        .then(
          (r) => live && setDescribed({ ok: r.ok, text: r.ok ? (r.human ?? "") : (r.error ?? "") }),
        )
        .catch(() => live && setDescribed(null));
    }, 250);
    return () => {
      live = false;
      clearTimeout(id);
    };
  }, [value, custom]);

  return (
    <div className="flex items-center gap-1.5">
      <label htmlFor="swarm-schedule" className="shrink-0 text-xs text-muted">
        Runs
      </label>
      {custom ? (
        <input
          id="swarm-schedule"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="0 9 * * 5"
          spellCheck={false}
          className="w-28 rounded border border-edge-strong bg-void px-2 py-1.5 font-mono text-xs text-ink placeholder:text-muted focus:border-tron focus:outline-none"
          title="A five-field cron expression. The server refuses one it cannot parse rather than saving a swarm that never fires."
        />
      ) : (
        <select
          id="swarm-schedule"
          value={value}
          onChange={(e) => {
            if (e.target.value === "__custom") {
              setCustom(true);
              return;
            }
            onChange(e.target.value);
          }}
          className="rounded border border-edge-strong bg-void px-2 py-1.5 text-xs text-ink focus:border-tron focus:outline-none"
        >
          {PRESETS.map((p) => (
            <option key={p.label} value={p.expr}>
              {p.label}
            </option>
          ))}
          <option value="__custom">Custom cron…</option>
        </select>
      )}
      {custom && described && (
        <span
          className={`shrink-0 text-[11px] ${described.ok ? "text-muted" : "text-danger"}`}
          title={described.ok ? value : undefined}
        >
          {described.text}
        </span>
      )}
      {custom && (
        <button
          onClick={() => {
            setCustom(false);
            onChange("");
          }}
          className="shrink-0 text-[11px] text-muted underline decoration-dotted underline-offset-2 hover:text-ink"
        >
          presets
        </button>
      )}
    </div>
  );
}

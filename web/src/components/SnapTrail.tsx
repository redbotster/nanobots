/** The one loud element in the whole UI — a snap between two bots' ports.
 * `live` pulses the trail while a run is actually moving data across it.
 * `extra` (when > 0) means more snaps connect this same pair of bots — the
 * connector can only show one label at a time, so it says "+N more" rather
 * than silently hiding the rest.
 *
 * `join` marks a snap that collapses a fanned-out list into one value
 * (docs/fan-out.md). Worth a label of its own: "twenty reminders became one
 * summary" is the most surprising thing a wire in this diagram can do, and
 * without it the trail looks like any other. */
export function SnapTrail({
  from,
  to,
  extra = 0,
  live,
  join,
}: {
  from: string;
  to: string;
  extra?: number;
  live?: boolean;
  join?: string;
}) {
  return (
    <div
      className="relative hidden h-24 w-28 shrink-0 sm:block"
      aria-label={
        `Snap: ${from} to ${to}` +
        (join ? `, joined into one value (${join})` : "") +
        (extra ? ` (+${extra} more)` : "")
      }
    >
      <svg
        viewBox="0 0 200 120"
        preserveAspectRatio="none"
        className="h-full w-full overflow-visible"
      >
        <path
          d="M0 52 C 60 52, 60 60, 100 60 S 140 68, 200 68"
          fill="none"
          strokeWidth={2}
          className="stroke-tron drop-shadow-[0_0_6px_var(--c-glow-shadow)]"
        />
        {live && (
          <path
            d="M0 52 C 60 52, 60 60, 100 60 S 140 68, 200 68"
            fill="none"
            strokeWidth={3}
            strokeDasharray="18 140"
            className="snap-pulse stroke-glow"
          />
        )}
      </svg>
      <span className="absolute left-1/2 top-2.5 -translate-x-1/2 whitespace-nowrap bg-void px-1.5 font-display text-[10px] text-glow">
        {from}
      </span>
      {join && (
        <span className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 whitespace-nowrap rounded border border-tron/50 bg-void px-1 font-display text-[9px] text-tron">
          join: {join}
        </span>
      )}
      <span className="absolute bottom-2 left-1/2 -translate-x-1/2 whitespace-nowrap bg-void px-1.5 font-display text-[10px] text-muted">
        {to}
        {extra > 0 && <span className="text-tron"> +{extra} more</span>}
      </span>
    </div>
  );
}

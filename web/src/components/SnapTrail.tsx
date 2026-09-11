/** The one loud element in the whole UI — a snap between two bots' ports.
 * `live` pulses the trail while a run is actually moving data across it. */
export function SnapTrail({
  from,
  to,
  live,
}: {
  from: string;
  to: string;
  live?: boolean;
}) {
  return (
    <div
      className="relative hidden h-24 w-28 shrink-0 sm:block"
      aria-label={`Snap: ${from} to ${to}`}
    >
      <svg viewBox="0 0 200 120" preserveAspectRatio="none" className="h-full w-full overflow-visible">
        <path
          d="M0 52 C 60 52, 60 60, 100 60 S 140 68, 200 68"
          fill="none"
          stroke="#00d4ff"
          strokeWidth={2}
          className="drop-shadow-[0_0_6px_rgba(0,212,255,0.8)]"
        />
        {live && (
          <path
            d="M0 52 C 60 52, 60 60, 100 60 S 140 68, 200 68"
            fill="none"
            stroke="#bff3ff"
            strokeWidth={3}
            strokeDasharray="18 140"
            className="snap-pulse"
          />
        )}
      </svg>
      <span className="absolute left-1/2 top-2.5 -translate-x-1/2 whitespace-nowrap bg-void px-1.5 font-display text-[10px] text-glow">
        {from}
      </span>
      <span className="absolute bottom-2 left-1/2 -translate-x-1/2 whitespace-nowrap bg-void px-1.5 font-display text-[10px] text-muted">
        {to}
      </span>
    </div>
  );
}

/** "14:32:07" — a run or Lab log line's own clock-time stamp, distinct from
 * relativeTime's "3m ago": a log is read as a sequence, where the wall-clock
 * time between two lines matters more than how long ago either one was. */
export function timeOf(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour12: false });
}

/** "3m ago", "2h ago", "just now" — falls back to a locale date once
 * something's more than a week old, since "9d ago" stops being more
 * useful than the actual date at that point. Used anywhere a timestamp
 * would otherwise force a reader to do the subtraction themselves. */
export function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return iso;
  const seconds = Math.round((Date.now() - then) / 1000);

  if (seconds < 10) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  if (days < 7) return `${days}d ago`;
  return new Date(iso).toLocaleDateString();
}

/** "in 3h", "in 2d", "any moment now" — the forward-looking twin of
 * relativeTime, for a scheduled swarm's next run. Same reasoning: a reader
 * shouldn't have to subtract a timestamp from now in their head. Falls back
 * to a locale date once something's more than a week out, where the actual
 * date beats "12d". */
export function untilTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return iso;
  const seconds = Math.round((then - Date.now()) / 1000);

  // A schedule that's due, or that just fired and hasn't been recomputed,
  // shouldn't render as "in -3s".
  if (seconds <= 30) return "any moment now";
  if (seconds < 60) return `in ${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `in ${minutes}m`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `in ${hours}h`;
  const days = Math.round(hours / 24);
  if (days < 7) return `in ${days}d`;
  return new Date(iso).toLocaleDateString();
}

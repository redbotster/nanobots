/** What a lazily-loaded page shows while its chunk arrives.
 *
 * Deliberately quiet. These chunks come off the same local machine in a few
 * milliseconds, so a spinner would flash more than it informs — and there
 * are six places that split now, which is exactly when two copies of this
 * start drifting into two different loading states.
 */
export function LazyFallback() {
  return <div className="p-6 text-sm text-muted/60">Loading…</div>;
}

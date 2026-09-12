import type { DraftBot, DraftSnap } from "../../lib/types";
import type { CanvasSnap, PlacedBot } from "./BuilderCanvas";

/**
 * Loading a saved swarm into the builder's own state.
 *
 * Extracted from BuilderPage and given a test for one reason: the builder
 * round-trips a *whole* swarm on every save, so anything this fails to load
 * is silently deleted when the human presses Save changes. That is not
 * hypothetical — it once removed a swarm's cron trigger, and `join` and
 * `on_error` were each one line away from the same fate.
 *
 * The server-side round-trip test covers the API. Nothing covered this
 * mapping, because it lived inside a React page.
 */
export function placeBots(bots: DraftBot[], position: (i: number) => { x: number; y: number }): PlacedBot[] {
  return bots.map((b, i) => {
    const [botId] = b.use.split("@");
    const pos = position(i);
    return { instanceId: b.id, botId, x: pos.x, y: pos.y, onError: b.on_error };
  });
}

export function toCanvasSnaps(snaps: DraftSnap[]): CanvasSnap[] {
  return snaps.map((s) => ({ from: s.from, to: s.to, join: s.join }));
}

import type { BotSummary, DraftBot, DraftSnap } from "../../lib/types";
import type { CanvasSnap, PlacedBot } from "./BuilderCanvas";

/**
 * Loading a saved swarm into the builder's own state.
 *
 * Extracted from BuilderPage and given a test for one reason: the builder
 * round-trips a *whole* swarm on every save, so anything this fails to load
 * is silently deleted when the human presses Save changes. That is not
 * hypothetical — it once removed a swarm's cron trigger, and `join` and
 * `on_error` were each one line away from the same fate. `retry`,
 * `execution`, `retry_backoff`, `when`, `fallback`, `loop` and `swarm`
 * were the same gap, unnoticed until now because nothing in the catalog
 * used any of them yet.
 *
 * The server-side round-trip test covers the API. Nothing covered this
 * mapping, because it lived inside a React page.
 */
export function placeBots(
  bots: DraftBot[],
  position: (i: number) => { x: number; y: number },
): PlacedBot[] {
  return bots.map((b, i) => {
    const [botId] = b.use.split("@");
    const pos = position(i);
    return {
      instanceId: b.id,
      botId,
      x: pos.x,
      y: pos.y,
      onError: b.on_error,
      retry: b.retry,
      retryBackoff: b.retry_backoff,
      when: b.when,
      fallback: b.fallback,
      loop: b.loop,
      swarm: b.swarm,
      execution: b.execution,
    };
  });
}

export function toCanvasSnaps(snaps: DraftSnap[]): CanvasSnap[] {
  return snaps.map((s) => ({ from: s.from, to: s.to, join: s.join }));
}

/**
 * The inverse of placeBots, for validate and save: one placed bot's wire
 * shape.
 *
 * `use:` is reconstructed from botId + the catalog's own version only when
 * the bot actually has a catalog botId. A nested-swarm bot (b.swarm set)
 * has none — sending a synthesized `use: "@0.0.0"` for one would silently
 * turn a swarm reference into a broken bot reference, which is worse than
 * the field simply going missing. Extracted and tested for the same reason
 * placeBots is: this used to be an inline object literal in BuilderPage,
 * duplicated between the validate effect and save.
 */
export function toDraftBot(
  b: PlacedBot,
  botDefs: Record<string, BotSummary>,
  inputs?: Record<string, unknown>,
): DraftBot {
  return {
    id: b.instanceId,
    use: b.swarm ? "" : `${b.botId}@${botDefs[b.botId]?.version ?? "0.0.0"}`,
    swarm: b.swarm,
    inputs,
    on_error: b.onError,
    retry: b.retry,
    retry_backoff: b.retryBackoff,
    when: b.when,
    fallback: b.fallback,
    loop: b.loop,
    execution: b.execution,
  };
}

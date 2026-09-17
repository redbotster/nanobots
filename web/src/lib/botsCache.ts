import { getIfChanged, Unchanged } from "./api";
import type { BotSummary } from "./types";

/**
 * The bot catalog, revalidated rather than re-downloaded.
 *
 * It is 30KB and changes only when someone flips a service to live or edits
 * a bot's instructions. Five surfaces fetch it on mount — the bot library,
 * Settings, the Team page, a swarm view and the builder — so navigating
 * between them cost 30KB a time: 61KB of a 336KB five-page browse, measured
 * in a browser, and the largest consumer left once the run list was behind a
 * conditional GET.
 *
 * The tag is held here rather than by the browser, for the same reason
 * runsFeed holds its own: these responses are `no-store`, so the browser
 * will not revalidate them on its own (see writeJSONCached).
 *
 * **There is nothing to invalidate, deliberately.** Every call goes to the
 * server; the held copy is only ever returned when the server has just said
 * 304, which it computes by hashing the body it would have sent. So a bot
 * changed a millisecond ago comes back changed, and no call site has to
 * remember anything.
 *
 * This started out with an `invalidateBots()` and three mutator wrappers so
 * the demo/live switch and the instructions editor could clear it. That was
 * wrong twice over: it protected against staleness this cache cannot have,
 * and dropping the tag made a save that changed nothing cost a full 30KB
 * where it would otherwise have been a 304.
 */
let etag: string | null = null;
let cached: BotSummary[] | null = null;

export async function listBotsCached(): Promise<BotSummary[]> {
  const res = await getIfChanged<BotSummary[]>("/api/bots", etag);
  if (res === Unchanged) {
    // Only reachable when a tag was sent, which only happens when there is
    // something cached to send it for.
    return cached ?? [];
  }
  etag = res.etag;
  cached = res.data;
  return res.data;
}

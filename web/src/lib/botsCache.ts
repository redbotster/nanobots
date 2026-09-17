import { api, getIfChanged, Unchanged } from "./api";
import type { BotSummary, SetProviderConnectionResult } from "./types";

/**
 * The bot catalog, fetched once and re-validated rather than re-downloaded.
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
 * Deliberately not a poller. Nothing changes the catalog except this app,
 * and every call that changes it goes through one of the three mutators
 * below rather than through `api` directly — a cache whose invalidation is
 * a rule call sites have to remember is a cache that serves a stale live/demo
 * switch the first time someone adds a fourth one.
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

/** Forget the cache after something in this app changed a bot. */
export function invalidateBots() {
  etag = null;
  cached = null;
}

// The three ways this app changes a bot. Each is `api.<name>` plus the
// invalidation, so the next `listBotsCached()` re-fetches rather than
// re-serving the shape the bot had before the edit.

export function setBotServiceConnection(
  botId: string,
  serviceId: string,
  live: boolean,
): Promise<BotSummary> {
  invalidateBots();
  return api.setBotServiceConnection(botId, serviceId, live);
}

export function setBotInstructions(botId: string, instructions: string): Promise<BotSummary> {
  invalidateBots();
  return api.setBotInstructions(botId, instructions);
}

/** One switch in Settings, flipping every bot that uses the service. */
export function setProviderConnection(
  service: string,
  live: boolean,
): Promise<SetProviderConnectionResult> {
  invalidateBots();
  return api.setProviderConnection(service, live);
}

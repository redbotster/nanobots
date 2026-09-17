import { revalidatingList } from "./revalidatingList";
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
 * Nothing here polls it and nothing invalidates it; see revalidatingList for
 * why neither is needed.
 */
const read = revalidatingList<BotSummary>("/api/bots");

export async function listBotsCached(): Promise<BotSummary[]> {
  return (await read()).data;
}

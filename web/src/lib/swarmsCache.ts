import { revalidatingList } from "./revalidatingList";
import type { SwarmSummary } from "./types";

/**
 * The swarm list, revalidated rather than re-downloaded.
 *
 * `GET /api/swarms` already answered 304, and the Swarms page already sent a
 * tag. The tag just lived inside that page's polling effect, so every way of
 * arriving at the list started again from nothing. Measured over a five-page
 * browse: four requests, three of them full 10KB responses — the shell's
 * count-up on mount, the poller's first tick, and the poller's first tick
 * *again* after a trip to Runs and back.
 *
 * `changed` is false when the server answered 304. It exists so the poller
 * can skip the state update entirely rather than setting state to an equal
 * value and relying on React to bail out — an idle Swarms page should cost
 * one empty round trip every four seconds and no render at all.
 */
const read = revalidatingList<SwarmSummary>("/api/swarms");

export async function listSwarmsCached(): Promise<{ swarms: SwarmSummary[]; changed: boolean }> {
  const res = await read();
  return { swarms: res.data, changed: res.changed };
}

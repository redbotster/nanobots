import { getIfChanged, Unchanged } from "./api";

/**
 * A list endpoint that is revalidated rather than re-downloaded.
 *
 * Two of this app's lists want exactly the same thing and had it written out
 * twice: `/api/bots` (30KB, fetched on mount by five surfaces) and
 * `/api/swarms` (10KB, fetched on mount *and* polled while the list is on
 * screen). Both carry an ETag; the client has to hold it, because these
 * responses are `no-store` and the browser will not revalidate them on its
 * own (see writeJSONCached).
 *
 * **Nothing here needs invalidating after a change.** Every read goes to the
 * server, and the held copy comes back only when the server has just said
 * 304 — which it decides by hashing the body it would have sent. So a bot
 * whose demo/live switch was flipped a millisecond ago comes back flipped.
 * The bot cache first shipped with an `invalidateBots()` and three mutator
 * wrappers, which guarded against staleness this shape cannot have and made
 * a save that changed nothing cost a full 30KB instead of a 304.
 *
 * Concurrent reads share one request. Measured on a five-page browse: the
 * shell's count-up and the Swarms page's first poll both mount at t=0, so
 * both sent no tag and both got the full 10KB. This is a hazard only if a
 * caller that has just changed something joins a request that started
 * before the change — and the two post-save callers both run while the
 * poller is torn down, so there is nothing in flight for them to join.
 */
export type ListRead<T> = { data: T[]; changed: boolean };

export function revalidatingList<T>(path: string): () => Promise<ListRead<T>> {
  let etag: string | null = null;
  let cached: T[] = [];
  let inFlight: Promise<ListRead<T>> | null = null;

  return function read(): Promise<ListRead<T>> {
    if (inFlight) return inFlight;
    inFlight = (async () => {
      const res = await getIfChanged<T[]>(path, etag);
      // A 304 before anything is cached cannot happen — no tag goes up
      // without a copy to go with it — but an empty list here would read as
      // "there are none", which is a different claim from "unchanged".
      if (res === Unchanged) return { data: cached, changed: false };
      etag = res.etag;
      cached = res.data;
      return { data: res.data, changed: true };
    })();
    return inFlight.finally(() => {
      inFlight = null;
    });
  };
}

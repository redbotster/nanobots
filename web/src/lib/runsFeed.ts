import { useEffect, useState } from "react";
import { getIfChanged, Unchanged } from "./api";
import type { RunSummary } from "./types";

/**
 * One poller for the run list, shared by everything that needs it.
 *
 * The Runs page and the approval notifier each used to poll /api/runs every
 * two seconds independently — the same request, twice, forever. This is a
 * single module-level loop they both subscribe to, so the request count
 * doesn't grow with the number of things interested in the answer.
 *
 * It also stops entirely when nothing is subscribed and when the tab is
 * hidden. nanobotd is a local process someone leaves running all day; a
 * background tab quietly polling twice a second is a real cost to pay for
 * data nobody is looking at. Coming back to the tab refetches immediately,
 * so it never shows something stale.
 */

const POLL_MS = 2000;
/** Ceiling for the backoff below. Low enough that the list catches up
 * within a few seconds of nanobotd coming back — the banner in the header
 * has already said it is gone, so this only governs how stale the list is
 * for a moment afterwards. */
const MAX_POLL_MS = 16000;

type Listener = (runs: RunSummary[]) => void;

const listeners = new Set<Listener>();
let timer: ReturnType<typeof setTimeout> | null = null;
let latest: RunSummary[] | null = null;
let visibilityBound = false;
let etag: string | null = null;
/** The current gap between polls. Doubles on each consecutive failure and
 * snaps back to POLL_MS on the first success.
 *
 * Without it, a daemon that is down — which is the normal state before
 * someone runs `nanobots up`, and every time it restarts — got a failed
 * request every two seconds for as long as the tab stayed open, filling the
 * console with proxy 500s and doing nothing useful with any of them. */
let delay = POLL_MS;

async function fetchOnce() {
  try {
    const res = await getIfChanged<RunSummary[]>("/api/runs", etag);
    delay = POLL_MS;
    // 304: same bytes as last time. Returning here is the whole win — no
    // parse, no new array, no listener call, so an idle Runs page stops
    // re-rendering itself every two seconds over data that did not move.
    if (res === Unchanged) return;
    etag = res.etag;
    latest = res.data;
    for (const fn of listeners) fn(res.data);
  } catch {
    // A failed poll is not an event — nanobotd restarting shouldn't blank
    // the list, and the header already says when it is unreachable. Back
    // off so an outage costs a handful of requests rather than one every
    // two seconds until the tab is closed.
    delay = Math.min(delay * 2, MAX_POLL_MS);
  }
}

function shouldPoll() {
  return listeners.size > 0 && document.visibilityState !== "hidden";
}

/** Whether a poll chain is alive. Separate from `timer`, which is null
 * while a request is actually in flight. */
let polling = false;

/** Which chain is the live one.
 *
 * `polling` alone was not enough, and the gap it left was measurable: the
 * feed ran two chains in lockstep, two identical requests 0ms apart every
 * two seconds, doubling this endpoint's traffic.
 *
 * Stopping cannot cancel a tick that is already awaiting its fetch —
 * clearTimeout has nothing to clear, because `timer` is null for exactly as
 * long as the request is in flight. So the stop set polling=false, the
 * in-flight tick finished and rescheduled itself regardless, and the next
 * subscribe saw polling=false and started a second chain beside it.
 *
 * React's StrictMode does subscribe/unsubscribe/subscribe on mount, which is
 * how this showed up — and is exactly what StrictMode is for. The race is
 * real without it too: any subscriber churn while a request is in flight
 * does the same thing.
 *
 * A tick now carries the epoch it was started in and retires quietly if that
 * is no longer current, so a stop orphans the in-flight chain instead of
 * leaving it running. */
let epoch = 0;

/** One poll, then schedule the next. A self-rescheduling timeout rather
 * than setInterval, because the gap is no longer constant — and because
 * setInterval would stack requests on top of each other if one were slow. */
async function tick(mine: number) {
  await fetchOnce();
  // Superseded while the request was in flight: another chain owns the
  // schedule now, or polling was stopped. Either way this one is done, and
  // must not touch `timer` or `polling` on the way out.
  if (mine !== epoch) return;
  if (shouldPoll()) {
    timer = setTimeout(() => void tick(mine), delay);
  } else {
    timer = null;
    polling = false;
  }
}

function sync() {
  if (shouldPoll() && !polling) {
    // Starting always tries immediately at full speed: whatever the backoff
    // had grown to, coming back to the tab means the user wants to see now.
    polling = true;
    delay = POLL_MS;
    epoch++;
    void tick(epoch);
  } else if (!shouldPoll() && polling) {
    if (timer !== null) clearTimeout(timer);
    timer = null;
    polling = false;
    // Orphans any tick still awaiting its fetch, which clearTimeout above
    // cannot reach.
    epoch++;
  }
}

function subscribe(fn: Listener): () => void {
  listeners.add(fn);
  if (latest) fn(latest); // a new subscriber shouldn't wait a tick to render
  if (!visibilityBound) {
    document.addEventListener("visibilitychange", sync);
    visibilityBound = true;
  }
  sync();
  return () => {
    listeners.delete(fn);
    sync();
  };
}

/** The shared run list. `null` until the first fetch lands, so callers can
 * tell "still loading" from "genuinely empty". */
export function useRuns(): RunSummary[] | null {
  const [runs, setRuns] = useState<RunSummary[] | null>(latest);
  useEffect(() => subscribe(setRuns), []);
  return runs;
}

/** Forces an immediate refetch — for right after starting or approving
 * something, where waiting up to two seconds to see it feels broken. */
export function refreshRuns() {
  // Drop the tag first: this is called right after an action the user took,
  // and the point is to see its effect immediately rather than to confirm
  // nothing changed.
  etag = null;
  delay = POLL_MS;
  void fetchOnce();
}

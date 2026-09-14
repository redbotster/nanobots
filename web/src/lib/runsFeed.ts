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

type Listener = (runs: RunSummary[]) => void;

const listeners = new Set<Listener>();
let timer: ReturnType<typeof setInterval> | null = null;
let latest: RunSummary[] | null = null;
let visibilityBound = false;
let etag: string | null = null;

async function fetchOnce() {
  try {
    const res = await getIfChanged<RunSummary[]>("/api/runs", etag);
    // 304: same bytes as last time. Returning here is the whole win — no
    // parse, no new array, no listener call, so an idle Runs page stops
    // re-rendering itself every two seconds over data that did not move.
    if (res === Unchanged) return;
    etag = res.etag;
    latest = res.data;
    for (const fn of listeners) fn(res.data);
  } catch {
    // A failed poll is not an event — nanobotd restarting shouldn't blank
    // the list. The next tick recovers.
  }
}

function shouldPoll() {
  return listeners.size > 0 && document.visibilityState !== "hidden";
}

function sync() {
  if (shouldPoll() && timer === null) {
    void fetchOnce();
    timer = setInterval(fetchOnce, POLL_MS);
  } else if (!shouldPoll() && timer !== null) {
    clearInterval(timer);
    timer = null;
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
  void fetchOnce();
}

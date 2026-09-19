import { useEffect, useState } from "react";
import { subscribeRunsEvents } from "./api";
import type { RunSummary } from "./types";

/**
 * One SSE connection for the run list, shared by everything that needs it.
 *
 * Replaces polling GET /api/runs every two seconds with an ETag (measured
 * at 2.1KB/14s idle — see docs/runs.md). The stream sends a snapshot on
 * connect and then one frame per run each time something the list renders
 * about it changes, so an idle tab costs nothing beyond the open
 * connection itself: no request, no 304, no re-render.
 *
 * Still a single shared subscription rather than one EventSource per
 * caller, and still closed entirely when nothing is subscribed or the tab
 * is hidden — an open stream is a goroutine and a socket on nanobotd for
 * as long as it's held, and a background tab looking at nothing shouldn't
 * hold one.
 */

type Listener = (runs: RunSummary[]) => void;

const listeners = new Set<Listener>();
const byId = new Map<string, RunSummary>();
let latest: RunSummary[] | null = null;
let unsubscribe: (() => void) | null = null;
let visibilityBound = false;

/** Same ordering as internal/api.sortedRuns: newest first, tie-broken by
 * id, so a fresh snapshot and a live delta never disagree about order. */
function compareRuns(a: RunSummary, b: RunSummary): number {
  if (a.started_at === b.started_at) {
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  }
  return a.started_at > b.started_at ? -1 : 1;
}

function publish() {
  latest = [...byId.values()].sort(compareRuns);
  for (const fn of listeners) fn(latest);
}

function shouldConnect() {
  return listeners.size > 0 && document.visibilityState !== "hidden";
}

function connect() {
  unsubscribe = subscribeRunsEvents((ev) => {
    if (ev.type === "snapshot") {
      byId.clear();
      for (const run of ev.runs) byId.set(run.id, run);
    } else {
      byId.set(ev.run.id, ev.run);
    }
    publish();
  });
}

function sync() {
  if (shouldConnect() && !unsubscribe) {
    connect();
  } else if (!shouldConnect() && unsubscribe) {
    unsubscribe();
    unsubscribe = null;
  }
}

function subscribe(fn: Listener): () => void {
  listeners.add(fn);
  if (latest) fn(latest); // a new subscriber shouldn't wait for the next event to render
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

/** The shared run list. `null` until the first snapshot lands, so callers
 * can tell "still loading" from "genuinely empty". */
export function useRuns(): RunSummary[] | null {
  const [runs, setRuns] = useState<RunSummary[] | null>(latest);
  useEffect(() => subscribe(setRuns), []);
  return runs;
}

/** No longer does anything: every write that used to need a forced
 * refetch (starting a run, deciding an approval) already broadcasts to
 * this stream the moment it happens, on the same connection every
 * subscriber already has open. Kept so callers written for the old
 * poll-and-refetch behavior don't need to change. */
export function refreshRuns() {}

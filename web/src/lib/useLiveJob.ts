import { useEffect, useRef, useState } from "react";
import type { LogEntry, LoggableJob } from "./types";

/** How often to re-poll for the parts SSE doesn't carry (status, approvals,
 * outputs). Short, because this only runs while one run is on screen. */
const POLL_MS = 1200;

/**
 * Tracks one long-running thing — a swarm run or a foundry job — over two
 * channels at once: polling for status/approvals/outputs, and SSE for the
 * log tail, so a new line appears the instant a step completes instead of
 * waiting up to a poll.
 *
 * The log deliberately comes from SSE alone once streaming starts. The
 * poll's own copy is dropped (`log: prev?.log ?? fresh.log`) so a slow
 * response can't rewind the tail to an older snapshot.
 *
 * useRun and useFoundryJob were this same function twice, differing only in
 * which two API calls they made — right down to the comments. They're both
 * thin wrappers now.
 */
export function useLiveJob<T extends LoggableJob & { status: string }>(
  id: string | null,
  fetchOne: (id: string) => Promise<T>,
  subscribe: (id: string, onEntry: (entry: LogEntry) => void) => () => void,
) {
  const [job, setJob] = useState<T | null>(null);
  const logRef = useRef<LogEntry[]>([]);

  // The callers pass inline arrows, so depending on them would restart the
  // subscription every render. `id` is the real identity of what's watched.
  const fetchRef = useRef(fetchOne);
  const subscribeRef = useRef(subscribe);
  fetchRef.current = fetchOne;
  subscribeRef.current = subscribe;

  useEffect(() => {
    if (!id) {
      setJob(null);
      return;
    }
    let cancelled = false;
    logRef.current = [];

    const poll = async () => {
      try {
        const fresh = await fetchRef.current(id);
        if (cancelled) return;
        setJob((prev) => ({ ...fresh, log: prev?.log ?? fresh.log }));
      } catch {
        // Transient — nanobotd restarting shouldn't blank the view. The
        // next tick recovers.
      }
    };
    void poll();
    const interval = setInterval(() => {
      if (!cancelled) void poll();
    }, POLL_MS);

    const unsubscribe = subscribeRef.current(id, (entry) => {
      if (cancelled) return;
      logRef.current = [...logRef.current, entry];
      setJob((prev) => (prev ? { ...prev, log: logRef.current } : prev));
    });

    return () => {
      cancelled = true;
      clearInterval(interval);
      unsubscribe();
    };
  }, [id]);

  const isTerminal = job?.status === "succeeded" || job?.status === "failed";
  return { job, isTerminal };
}

import { useEffect, useRef, useState } from "react";
import { api, subscribeRunEvents } from "./api";
import type { LogEntry, Run } from "./types";

/** Tracks one run's live state: polls for status/approvals (the parts SSE
 * doesn't carry) and subscribes to SSE for the log tail, so new lines show
 * up the instant a step completes rather than waiting for the next poll. */
export function useRun(runId: string | null) {
  const [run, setRun] = useState<Run | null>(null);
  const logRef = useRef<LogEntry[]>([]);

  useEffect(() => {
    if (!runId) {
      setRun(null);
      return;
    }
    let cancelled = false;
    logRef.current = [];

    const poll = async () => {
      try {
        const r = await api.getRun(runId);
        if (cancelled) return;
        setRun((prev) => ({ ...r, log: prev?.log ?? r.log }));
      } catch {
        // transient — the next tick retries
      }
    };
    poll();
    const interval = setInterval(() => {
      if (!cancelled) void poll();
    }, 1200);

    const unsubscribe = subscribeRunEvents(runId, (entry) => {
      if (cancelled) return;
      logRef.current = [...logRef.current, entry];
      setRun((prev) => (prev ? { ...prev, log: logRef.current } : prev));
    });

    return () => {
      cancelled = true;
      clearInterval(interval);
      unsubscribe();
    };
  }, [runId]);

  const isTerminal = run?.status === "succeeded" || run?.status === "failed";
  return { run, isTerminal };
}

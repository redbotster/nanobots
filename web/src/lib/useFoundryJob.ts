import { useEffect, useRef, useState } from "react";
import { api, subscribeFoundryEvents } from "./api";
import type { FoundryJob, LogEntry } from "./types";

/** Mirrors useRun's dual-channel poll+SSE pattern exactly, for a foundry
 * job instead of a swarm run — see useRun.ts. */
export function useFoundryJob(jobId: string | null) {
  const [job, setJob] = useState<FoundryJob | null>(null);
  const logRef = useRef<LogEntry[]>([]);

  useEffect(() => {
    if (!jobId) {
      setJob(null);
      return;
    }
    let cancelled = false;
    logRef.current = [];

    const poll = async () => {
      try {
        const j = await api.getFoundryJob(jobId);
        if (cancelled) return;
        setJob((prev) => ({ ...j, log: prev?.log ?? j.log }));
      } catch {
        // transient — the next tick retries
      }
    };
    poll();
    const interval = setInterval(() => {
      if (!cancelled) void poll();
    }, 1200);

    const unsubscribe = subscribeFoundryEvents(jobId, (entry) => {
      if (cancelled) return;
      logRef.current = [...logRef.current, entry];
      setJob((prev) => (prev ? { ...prev, log: logRef.current } : prev));
    });

    return () => {
      cancelled = true;
      clearInterval(interval);
      unsubscribe();
    };
  }, [jobId]);

  const isTerminal = job?.status === "succeeded" || job?.status === "failed";
  return { job, isTerminal };
}

import { useEffect, useState } from "react";
import { api } from "./api";
import type { StatusResponse } from "./types";

/** Polls /api/status instead of fetching it once at mount. Both things it
 * reports can change while a tab sits open — you go start Docker Desktop,
 * or you paste a 1Claw key — and a banner that says "Docker isn't running"
 * long after you started it is worse than no banner at all. The server
 * caches the Docker probe, so this is cheap. */
export function useStatus(intervalMs = 8000) {
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [unreachable, setUnreachable] = useState(false);

  useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .status()
        .then((s) => {
          if (!alive) return;
          setStatus(s);
          setUnreachable(false);
        })
        .catch(() => alive && setUnreachable(true));
    load();
    const id = setInterval(load, intervalMs);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, [intervalMs]);

  return { status, unreachable };
}

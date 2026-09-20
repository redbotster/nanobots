import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { useRuns } from "./runsFeed";
import type { FoundryJob } from "./types";

/** Foundry jobs are a rare escalation path, not an everyday surface, and a
 * job runs for minutes — polling them as hard as swarm runs bought nothing. */
const FOUNDRY_POLL_MS = 6000;

/** Anything waiting on a human decision, across swarm runs and foundry jobs,
 * independent of whichever page is open — the nav badge needs it everywhere.
 *
 * Runs come from the shared feed (see runsFeed.ts) rather than a second
 * poller of the same endpoint, so this hook costs one request per six
 * seconds now instead of one per two seconds on top of whatever the Runs
 * page was already doing.
 *
 * The browser notification fires only when the combined count *increases*,
 * so approving one thing doesn't immediately notify you about the next
 * unrelated one still pending. */
export function useApprovalNotifications() {
  const runs = useRuns();
  // The jobs themselves, not just a count — the nav badge used to add this
  // to the swarm-run count while nothing on the Runs page it points to
  // could show a foundry job at all: the badge said "2", the page you
  // landed on said "1", and the second thing waiting on you had no way to
  // reach it once you'd navigated away from the exact escalation screen
  // that first opened it. RunsPage renders these in their own banner now
  // (see pendingFoundryJobs there) so the badge's count and what clicking
  // it shows agree.
  const [pendingFoundryJobs, setPendingFoundryJobs] = useState<FoundryJob[]>([]);
  const [permission, setPermission] = useState<NotificationPermission | "unsupported">(
    "Notification" in window ? Notification.permission : "unsupported",
  );
  const prevCount = useRef(0);

  useEffect(() => {
    let alive = true;
    const check = () =>
      api
        .listFoundryJobs()
        .then((jobs) => {
          if (alive) {
            setPendingFoundryJobs(jobs.filter((j) => j.status === "awaiting_approval"));
          }
        })
        .catch(() => {});
    check();
    const id = setInterval(check, FOUNDRY_POLL_MS);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, []);

  const count =
    (runs ?? []).filter((r) => r.status === "awaiting_approval").length + pendingFoundryJobs.length;

  useEffect(() => {
    if (count > prevCount.current && permission === "granted") {
      new Notification("Nanobots needs your approval", {
        body: count === 1 ? "One thing is waiting on you." : `${count} things are waiting on you.`,
      });
    }
    prevCount.current = count;
  }, [count, permission]);

  const requestPermission = () => {
    if (!("Notification" in window)) return;
    Notification.requestPermission().then(setPermission);
  };

  return { count, pendingFoundryJobs, permission, requestPermission };
}

import { useEffect, useRef, useState } from "react";
import { api } from "./api";

/** Polls both swarm runs and foundry jobs for anything awaiting_approval —
 * independent of whichever page is open, the same reason App.tsx's own nav
 * badge already polls this way — and fires a real browser notification
 * the moment the combined count *increases*, not on every poll, so
 * approving one thing doesn't immediately notify you about the next
 * unrelated one still pending. Returns the live count for the nav badge
 * plus whether notifications are actually available/granted, so the
 * header can offer to turn them on. */
export function useApprovalNotifications() {
  const [count, setCount] = useState(0);
  const [permission, setPermission] = useState<NotificationPermission | "unsupported">(
    "Notification" in window ? Notification.permission : "unsupported",
  );
  const prevCount = useRef(0);

  useEffect(() => {
    const check = async () => {
      const [runs, jobs] = await Promise.all([
        api.listRuns().catch(() => []),
        api.listFoundryJobs().catch(() => []),
      ]);
      const total =
        runs.filter((r) => r.status === "awaiting_approval").length +
        jobs.filter((j) => j.status === "awaiting_approval").length;

      if (total > prevCount.current && permission === "granted") {
        new Notification("Nanobots needs your approval", {
          body: total === 1 ? "One thing is waiting on you." : `${total} things are waiting on you.`,
        });
      }
      prevCount.current = total;
      setCount(total);
    };
    check();
    const id = setInterval(check, 2000);
    return () => clearInterval(id);
  }, [permission]);

  const requestPermission = () => {
    if (!("Notification" in window)) return;
    Notification.requestPermission().then(setPermission);
  };

  return { count, permission, requestPermission };
}

import type { StatusResponse } from "../lib/types";

/** Every bot runs in a container, so a stopped Docker makes the entire
 * product a no-op. Before this, you found out by clicking Run, waiting, and
 * reading "cannot connect to the Docker daemon at unix://…" at the bottom of
 * a log. One line above the page says it up front, and disappears on its own
 * once /api/status (polled) sees the daemon come back. */
export function DockerBanner({ status }: { status: StatusResponse | null }) {
  if (!status || status.docker_available) return null;
  const installed = status.docker_reason !== "Docker isn't installed";
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 border-b border-warn/40 bg-warn/[0.08] px-4 py-1.5 text-[12px] text-warn sm:px-6">
      <span className="font-medium">
        {status.docker_reason || "Docker isn't available"}
      </span>
      <span className="text-warn/80">
        {installed
          ? "— bots run in containers, so nothing can run until you start Docker Desktop."
          : "— bots run in containers. Install Docker Desktop to run anything for real."}
      </span>
    </div>
  );
}

import type { StatusResponse } from "../lib/types";

function Banner({ headline, detail }: { headline: string; detail: string }) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 border-b border-warn/40 bg-warn/[0.08] px-4 py-1.5 text-[12px] text-warn sm:px-6">
      <span className="font-medium">{headline}</span>
      <span className="text-warn/80">{detail}</span>
    </div>
  );
}

/** The two prerequisites that silently break everything, reported before you
 * press Run rather than several steps into a failed run.
 *
 * Docker: every bot runs in a container, so a stopped daemon makes the whole
 * product a no-op — you used to find out by clicking Run, waiting, and
 * reading "cannot connect to the Docker daemon" at the bottom of a log.
 *
 * The 1Claw vault: it re-locks on its own schedule, and while locked every
 * bot holding a Slack/GitHub/Stripe/HubSpot credential fails — typically as
 * the last bot in a swarm, after the earlier ones have already sent mail or
 * written to Drive for real.
 *
 * Both clear themselves once /api/status (polled) sees the problem go away. */
export function PrereqBanners({ status }: { status: StatusResponse | null }) {
  if (!status) return null;
  return (
    <>
      {!status.docker_available && (
        <Banner
          headline={status.docker_reason || "Docker isn't available"}
          detail={
            status.docker_reason === "Docker isn't installed"
              ? "— bots run in containers. Install Docker Desktop to run anything for real."
              : "— bots run in containers, so nothing can run until you start Docker Desktop."
          }
        />
      )}
      {status.vault_locked && (
        <Banner
          headline="1Claw vault is locked"
          detail={
            status.vault_reason ||
            "— unlock it with your passkey; bots using Slack, GitHub, Stripe or HubSpot will fail until you do."
          }
        />
      )}
    </>
  );
}

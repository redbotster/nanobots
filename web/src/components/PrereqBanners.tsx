import type { StatusResponse } from "../lib/types";

function Banner({
  headline,
  detail,
  action,
}: {
  headline: string;
  detail: string;
  /** A banner that names a problem without offering the fix makes the
   * reader go hunting. Where the fix is one click away, it goes here. */
  action?: { label: string; onClick: () => void };
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 border-b border-warn/40 bg-warn/[0.08] px-4 py-1.5 text-[12px] text-warn sm:px-6">
      <span className="font-medium">{headline}</span>
      <span className="text-warn/80">{detail}</span>
      {action && (
        <button
          onClick={action.onClick}
          className="rounded border border-warn/50 px-1.5 py-0.5 text-warn transition-colors hover:bg-warn/10"
        >
          {action.label}
        </button>
      )}
    </div>
  );
}

/** The prerequisites that silently break everything, reported before you
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
export function PrereqBanners({
  status,
  onOpenSettings,
}: {
  status: StatusResponse | null;
  onOpenSettings?: () => void;
}) {
  if (!status) return null;
  return (
    <>
      {status.llm_backend === "none" && (
        <Banner
          headline="No model configured"
          detail="— every bot will return its demo fixtures, which look exactly like real output."
          action={onOpenSettings && { label: "Set one up", onClick: onOpenSettings }}
        />
      )}
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

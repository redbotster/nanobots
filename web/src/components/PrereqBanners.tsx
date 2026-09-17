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

/** The prerequisites that silently break things, reported before you press
 * Run rather than several steps into a failed run.
 *
 * Docker: it used to be all-or-nothing, and this banner said so — "nothing
 * can run until you start Docker Desktop". That stopped being true when 34
 * of the 39 bots moved in-process (internal/runner/inprocess.go), and a
 * banner announcing the product is dead when almost all of it works is a
 * worse bug than the missing dependency it is reporting. It now says what
 * Docker actually costs you: the bots that render a PDF or a chart.
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
              ? "— most bots run without it. The ones that render a PDF or a chart need Docker Desktop."
              : "— most bots run without it. The ones that render a PDF or a chart need it started."
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

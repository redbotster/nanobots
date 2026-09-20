import * as Tooltip from "@radix-ui/react-tooltip";

/** "wired" — a real snap feeds this port (input) or reads it (output).
 * "unfed" — a *required* input with nothing supplying it at all: not a
 * snap, not a default. The one state worth a person's attention; the run
 * fails here the moment it starts.
 * "unwired" — everything else: an optional input nobody set, or an output
 * nothing in this swarm happens to read. Neither is wrong. */
export type PortState = "wired" | "unfed" | "unwired";

const DOT_CLASS: Record<PortState, string> = {
  wired: "border-tron bg-void shadow-[0_0_6px_theme(colors.tron)]",
  unfed: "border-danger bg-danger/20 shadow-[0_0_6px_theme(colors.danger)]",
  unwired: "border-muted bg-void",
};

const STATE_LABEL: Record<PortState, string> = {
  wired: "connected",
  unfed: "required — nothing supplies it",
  unwired: "not connected",
};

export function PortBadge({
  name,
  type,
  state = "unwired",
}: {
  name: string;
  type: string;
  /** Defaults to "unwired" (the old `dim` look) for any caller that
   * hasn't computed real wiring — this component has no way to know
   * whether a port is connected on its own, only what its caller tells
   * it. See SwarmView's wiringByInstance for where that comes from. */
  state?: PortState;
}) {
  return (
    <Tooltip.Provider delayDuration={150}>
      <Tooltip.Root>
        <Tooltip.Trigger asChild>
          <span
            className={`inline-block h-3 w-3 cursor-help rounded-full border-2 ${DOT_CLASS[state]}`}
          />
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            sideOffset={6}
            className="rounded border border-edge-strong bg-panel-2 px-2 py-1 font-display text-xs text-ink shadow-glow-sm"
          >
            {name}: {type} — {STATE_LABEL[state]}
            <Tooltip.Arrow className="fill-panel-2" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

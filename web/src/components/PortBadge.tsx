import * as Tooltip from "@radix-ui/react-tooltip";

export function PortBadge({
  name,
  type,
  dim,
}: {
  name: string;
  type: string;
  dim?: boolean;
}) {
  return (
    <Tooltip.Provider delayDuration={150}>
      <Tooltip.Root>
        <Tooltip.Trigger asChild>
          <span
            className={`inline-block h-3 w-3 cursor-help rounded-full border-2 ${
              dim
                ? "border-muted"
                : "border-tron shadow-[0_0_6px_theme(colors.tron)]"
            } bg-void`}
          />
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            sideOffset={6}
            className="rounded border border-edge-strong bg-panel-2 px-2 py-1 font-display text-xs text-ink shadow-glow-sm"
          >
            {name}: {type}
            <Tooltip.Arrow className="fill-panel-2" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

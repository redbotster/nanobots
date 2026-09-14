import * as RadixSwitch from "@radix-ui/react-switch";

export function Switch({
  checked,
  onCheckedChange,
  label,
  ariaLabel,
  labelClassName = "",
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label?: string;
  /** The accessible name when the visible text lives outside this
   * component. The Bot library renders each service's name as a sibling of
   * the switch, so thirty-one connect toggles — the controls that hand a
   * bot a real account — all announced as "switch, not pressed" with
   * nothing to tell them apart. */
  ariaLabel?: string;
  /** Lets a caller hide the text label at narrow widths (the switch itself
   * still reads fine on its own) without duplicating this component. */
  labelClassName?: string;
}) {
  return (
    <label className="flex items-center gap-2 text-xs text-muted">
      {label && <span className={labelClassName}>{label}</span>}
      <RadixSwitch.Root
        checked={checked}
        onCheckedChange={onCheckedChange}
        // A <label> wrapping a button doesn't associate the two, and below
        // sm the text is display:none — so the one control that reveals the
        // Bot library announced as an unnamed switch.
        aria-label={ariaLabel ?? label}
        className="relative h-5 w-9 shrink-0 rounded-full bg-panel-2 outline-none transition-colors data-[state=checked]:bg-tron/60"
      >
        <RadixSwitch.Thumb className="block h-3.5 w-3.5 translate-x-1 rounded-full bg-ink transition-transform data-[state=checked]:translate-x-[18px]" />
      </RadixSwitch.Root>
    </label>
  );
}

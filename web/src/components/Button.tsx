import type { ButtonHTMLAttributes } from "react";

type Variant = "primary" | "ghost" | "danger";

// whitespace-nowrap because a squeezed flex row would otherwise break a
// label mid-phrase rather than letting the row wrap: at 375px the swarm
// page's header turned "Add a shared swarm" into three stacked lines in a
// tall thin box, and "Build manually" into two. A button label is a name,
// not a paragraph — it should push the layout around it instead of folding.
const base =
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md px-3.5 py-2 text-sm font-display font-semibold tracking-wide transition-colors disabled:opacity-40 disabled:cursor-not-allowed focus-visible:outline focus-visible:outline-2 focus-visible:outline-tron focus-visible:outline-offset-2";

const variants: Record<Variant, string> = {
  // text-white, not text-ink: `deep` is a saturated blue in both themes,
  // so the label on it stays white even when the page is light.
  primary: "bg-deep text-white hover:brightness-110 shadow-glow-sm",
  ghost:
    "border border-edge-strong text-ink hover:bg-tron/10 hover:border-tron",
  danger: "border border-danger/50 text-danger hover:bg-danger/10",
};

export function Button({
  variant = "ghost",
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }) {
  return (
    <button className={`${base} ${variants[variant]} ${className}`} {...props} />
  );
}

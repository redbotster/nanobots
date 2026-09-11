const colors: Record<string, string> = {
  ok: "bg-ok shadow-[0_0_8px_theme(colors.ok)]",
  warn: "bg-warn shadow-[0_0_8px_theme(colors.warn)]",
  danger: "bg-danger shadow-[0_0_8px_theme(colors.danger)]",
  muted: "bg-muted",
};

export function StatusDot({ tone = "muted" }: { tone?: keyof typeof colors }) {
  return <span className={`inline-block h-2 w-2 rounded-full ${colors[tone]}`} />;
}

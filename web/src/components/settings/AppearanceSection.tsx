import { useTheme, type Theme } from "../../lib/theme";

const THEMES: { value: Theme; label: string; icon: string }[] = [
  { value: "system", label: "System", icon: "🖥" },
  { value: "light", label: "Light", icon: "☀" },
  { value: "dark", label: "Dark", icon: "☾" },
];

/** The header's one-tap toggle can only ever pick light or dark. This is
 * where "just follow my OS" lives — the default, and the only one of the
 * three that keeps changing after you set it. */
export function AppearanceSection() {
  const { theme, resolved, setTheme } = useTheme();
  return (
    <section className="mt-5 rounded-lg border border-edge-strong bg-panel p-4">
      <h2 className="font-display text-sm font-semibold text-ink">Appearance</h2>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <div className="inline-flex overflow-hidden rounded-lg border border-edge-strong">
          {THEMES.map((t) => (
            <button
              key={t.value}
              onClick={() => setTheme(t.value)}
              aria-pressed={theme === t.value}
              className={`px-3 py-1.5 text-xs transition-colors ${
                theme === t.value
                  ? "bg-tron/20 font-medium text-ink"
                  : "text-muted hover:bg-panel-2 hover:text-ink"
              }`}
            >
              <span className="mr-1.5">{t.icon}</span>
              {t.label}
            </button>
          ))}
        </div>
        {theme === "system" && (
          <span className="text-[12px] text-muted">
            currently {resolved}, following your OS
          </span>
        )}
      </div>
    </section>
  );
}

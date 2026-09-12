import { useCallback, useEffect, useState } from "react";

/** "system" means no data-theme attribute at all, letting index.css's
 * prefers-color-scheme block decide — so the app follows the OS live,
 * including a mid-session switch, with no JS involved. */
export type Theme = "system" | "light" | "dark";

const KEY = "nanobots.theme";

export function readStoredTheme(): Theme {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "light" || v === "dark" || v === "system") return v;
  } catch {
    // Private windows and blocked site data both throw here; "system" is
    // the right answer in that case anyway.
  }
  return "system";
}

export function applyTheme(theme: Theme) {
  const root = document.documentElement;
  if (theme === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", theme);
}

/** What the user actually sees right now, which for "system" depends on the
 * OS and can change without any interaction here. */
export function resolvedTheme(theme: Theme): "light" | "dark" {
  if (theme !== "system") return theme;
  return window.matchMedia?.("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

/** Every useTheme() caller subscribes to this, so they can't disagree.
 *
 * The header toggle and the Settings three-way each called useTheme() and
 * each kept the answer in component-local state, with nothing to tell the
 * other it had changed. Picking Light in Settings left the header still
 * rendering ☀ and titled "Dark theme — click for light"; clicking it then
 * did nothing visible at all, because it set the theme to the value the
 * page was already on and only corrected its own label. Two clicks to
 * change the theme, the first with no feedback. */
const subscribers = new Set<() => void>();

function broadcast() {
  for (const fn of subscribers) fn();
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme);
  const [resolved, setResolved] = useState<"light" | "dark">(() =>
    resolvedTheme(readStoredTheme()),
  );

  useEffect(() => {
    const sync = () => {
      const current = readStoredTheme();
      setThemeState(current);
      setResolved(resolvedTheme(current));
    };
    subscribers.add(sync);
    sync(); // catch a change made before this instance mounted
    return () => {
      subscribers.delete(sync);
    };
  }, []);

  const setTheme = useCallback((next: Theme) => {
    applyTheme(next);
    try {
      localStorage.setItem(KEY, next);
    } catch {
      // Not being able to remember the choice shouldn't stop it applying.
    }
    // Written first, then broadcast: every subscriber re-reads storage, so
    // this instance and every other one land on the same value.
    broadcast();
  }, []);

  // On "system", track the OS so the header's icon doesn't go stale when
  // someone flips their Mac to light mode with this tab open.
  useEffect(() => {
    if (theme !== "system") return;
    const mq = window.matchMedia?.("(prefers-color-scheme: light)");
    if (!mq) return;
    const onChange = () => setResolved(mq.matches ? "light" : "dark");
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [theme]);

  return { theme, resolved, setTheme };
}

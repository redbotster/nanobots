import { useState } from "react";

export type UIMode = "basic" | "advanced";

const STORAGE_KEY = "nanobots:ui-mode";

/** Basic (the default for a new user) hides the bot library and the "start
 * from a blank canvas" entry point — the AI composer and the swarm gallery
 * cover everything a novice needs, with nothing to learn first. Advanced
 * reveals both, unchanged from how they've always worked. This never
 * changes *how* anything runs, only which entry points are visible — see
 * the design note in BuilderPage.tsx and README.md's UX section. */
export function useUIMode(): [UIMode, (mode: UIMode) => void] {
  const [mode, setModeState] = useState<UIMode>(() => {
    try {
      return localStorage.getItem(STORAGE_KEY) === "advanced" ? "advanced" : "basic";
    } catch {
      return "basic";
    }
  });

  const setMode = (next: UIMode) => {
    setModeState(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // A private window or blocked storage just won't remember the choice
      // across reloads — not worth failing over.
    }
  };

  return [mode, setMode];
}

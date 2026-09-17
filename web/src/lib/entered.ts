import { useState } from "react";

const STORAGE_KEY = "nanobots:entered";

/** Whether this browser has already gone past the landing page.
 *
 * There is no login here and never has been: the button said "Login", and
 * clicking it flipped a React state — no session, no identity, no
 * credential (nanobotd's own 1Claw key is the only credential in the
 * system). So it asked for nothing, protected nothing, and had to be
 * clicked again on every reload. A browser pass of the first run tripped
 * over it as a wall before the product.
 *
 * Remembering the choice is what makes the second visit land on Swarms. The
 * logo in the dashboard header goes back to the landing page, so this is not
 * a one-way door.
 *
 * When Nanobots is ever hosted for more than one person, this is where "Sign
 * in with 1Claw" (OAuth + PKCE, blueprint §3.2) replaces it — and then the
 * label can honestly say Login again. */
export function useEntered(): [boolean, (entered: boolean) => void] {
  const [entered, setEnteredState] = useState<boolean>(() => {
    try {
      return localStorage.getItem(STORAGE_KEY) === "yes";
    } catch {
      // A private window or blocked storage just shows the landing page
      // every time, which is the old behaviour and not worth failing over.
      return false;
    }
  });

  const setEntered = (next: boolean) => {
    setEnteredState(next);
    try {
      if (next) localStorage.setItem(STORAGE_KEY, "yes");
      else localStorage.removeItem(STORAGE_KEY);
    } catch {
      // Same: remembering is a convenience, not a requirement.
    }
  };

  return [entered, setEntered];
}

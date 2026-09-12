/**
 * Turns a run's raw error into the part a person actually needs.
 *
 * Errors arrive wrapped in the whole call chain that produced them:
 *
 *   container exited 1: nanobot-agent: error: bot notify: step "send":
 *   callback /internal/steps/notify: 1Claw vault is locked: Passkey
 *   verification required to access vault secrets.
 *
 * In a list row that means ~90 characters of plumbing before the first word
 * that tells you anything, and the actual reason gets truncated off the end.
 * Every layer of that prefix is real and worth keeping in the run detail and
 * the log — it just isn't what belongs in a one-line summary.
 *
 * The bot and step are kept, because "which bot failed" is genuinely useful
 * at a glance; the transport layers are not.
 */

const NOISE = [
  /^container exited -?\d+:\s*/,
  /^nanobot-agent:\s*/,
  /^error:\s*/,
  /^callback \/internal\/steps\/[a-z_]+:\s*/,
  /^resolve inputs:\s*/,
];

/** Pulls `bot notify: step "send":` off the front, returning it separately. */
function takeOrigin(s: string): { origin: string; rest: string } {
  let origin = "";
  let rest = s;

  const bot = rest.match(/^bot ([\w-]+):\s*/);
  if (bot) {
    origin = bot[1];
    rest = rest.slice(bot[0].length);
  }
  const step = rest.match(/^step "([^"]+)":\s*/);
  if (step) {
    origin = origin ? `${origin}/${step[1]}` : step[1];
    rest = rest.slice(step[0].length);
  }
  return { origin, rest };
}

export interface RunErrorParts {
  /** "notify/send", or "" when the error didn't come from a specific step. */
  origin: string;
  /** The reason, with transport wrappers stripped. Never empty for a
   * non-empty input — if nothing matches, this is the original string. */
  message: string;
}

export function parseRunError(raw: string | undefined | null): RunErrorParts | null {
  if (!raw) return null;
  let s = raw.trim();
  if (!s) return null;

  let origin = "";
  // Prefixes nest and repeat (a doubled "container exited 1:" was a real
  // bug), so keep peeling until nothing matches rather than assuming an
  // order or a depth.
  for (let i = 0; i < 12; i++) {
    const before = s;
    for (const re of NOISE) s = s.replace(re, "");
    const taken = takeOrigin(s);
    if (taken.origin && !origin) origin = taken.origin;
    s = taken.rest;
    if (s === before) break;
  }

  // Only ever the first line: a Go error can carry a multi-line body, and
  // the row has room for one.
  const message = s.split("\n")[0].trim() || raw.trim();
  return { origin, message };
}

/** One line for a list row: "notify/send — 1Claw vault is locked: …". */
export function shortRunError(raw: string | undefined | null): string {
  const parts = parseRunError(raw);
  if (!parts) return "";
  return parts.origin ? `${parts.origin} — ${parts.message}` : parts.message;
}

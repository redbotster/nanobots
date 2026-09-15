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

/**
 * The fix, when a failure has an obvious one.
 *
 * A run that went red tells you what happened. It rarely tells you what to
 * do, and in this product the answer is usually one click away in Settings
 * — six of the fifteen catalog swarms fail on a single missing Slack token,
 * and the error politely says "connect it from Settings" while leaving you
 * to go and find it.
 *
 * Deliberately conservative. Every entry here is a failure actually seen in
 * this build, matched on wording the server controls; anything unrecognised
 * gets no remedy rather than a guess, because a confidently wrong
 * suggestion is worse than none — it sends someone to reconfigure a thing
 * that was never the problem.
 */
export interface RunRemedy {
  /** One sentence: what to do about it. */
  advice: string;
  /** A Settings-level fix, when there is one. */
  action?: { label: string; page: "settings" };
  /** Where to read more, when the fix isn't a button. */
  docs?: string;
}

/** Services with a connect flow in Settings, as the server names them. */
const CONNECTABLE: Record<string, string> = {
  google: "Google",
  slack: "Slack",
  github: "GitHub",
  stripe: "Stripe",
  hubspot: "HubSpot",
  x: "X",
  linkedin: "LinkedIn",
};

export function runRemedy(raw: string | undefined | null): RunRemedy | null {
  const parts = parseRunError(raw);
  if (!parts) return null;
  const m = `${parts.message}`.toLowerCase();

  // A missing credential names the service in the secret path, e.g.
  // `Secret slack/bot_token not found`.
  const secret = m.match(/secret ([a-z_]+)\//);
  const service = secret && CONNECTABLE[secret[1]];
  if (service || m.includes("no connected account yet")) {
    return {
      advice: service
        ? `No ${service} account is connected yet, so this bot had no credential to use.`
        : "No account is connected for this service yet, so this bot had no credential to use.",
      action: { label: service ? `Connect ${service}` : "Open Settings", page: "settings" },
    };
  }

  // The vault re-locks on its own schedule and the fix is on a phone, so
  // there is no button to offer — only the right instruction.
  if (m.includes("vault is locked") || m.includes("passkey verification required")) {
    return {
      advice:
        "The 1Claw vault re-locked. Unlock it with your passkey on your phone, then run this again — nothing needs changing here.",
    };
  }

  if (m.includes("cannot connect to the docker daemon") || m.includes("docker isn't")) {
    return { advice: "Every bot runs in a container. Start Docker Desktop and run this again." };
  }

  if (m.includes("no llm is configured")) {
    return {
      advice: "No model is configured, so this bot had nothing to generate with.",
      action: { label: "Set up a model", page: "settings" },
    };
  }

  // Checked before the general "not approved" below: an unattended
  // decline is also "not approved", and the generic advice ("run it again
  // to be asked afresh") is wrong when there was never anything to ask.
  if (m.includes("no terminal attached")) {
    return {
      advice:
        "Nothing was attached to answer the approval, so it was declined automatically. Run it from here, or from a terminal where you can answer.",
    };
  }

  // Nobody was there. Distinct from a decline (a decision) and from a
  // hang (a fault): the run did everything right and then waited for a
  // person who never came.
  //
  // This is the single most common failure on a machine running scheduled
  // swarms, and it used to surface as "container exceeded 30m0s and was
  // stopped" — 54 runs on this one, every one of them an unanswered
  // approval, all of them reading as a hung bot. The advice names the two
  // real fixes, because "run it again" is not one: the next scheduled run
  // at 7am will go unanswered exactly the same way.
  if (m.includes("nobody answered")) {
    return {
      advice:
        "The bot asked for approval and nobody answered in time, so it was stopped. " +
        "If this runs on a schedule while you're away, connect 1Claw so the question " +
        "reaches your phone — or take the approval off this step if it doesn't need one.",
      action: { label: "Open Settings", page: "settings" },
      docs: "docs/approvals.md",
    };
  }

  // A declined approval is a decision, not a fault — saying so stops
  // someone debugging their own "no".
  if (m.includes("not approved")) {
    return {
      advice:
        "This wasn't a fault: the approval was declined, so the bot stopped before doing anything. Run it again to be asked afresh.",
    };
  }

  if (m.includes("has no explicit value, snap, or default")) {
    return {
      advice:
        "A bot needs an input nothing supplies. Give it a value on the bot, or snap it from an upstream bot, in the builder.",
    };
  }

  if (m.includes("they iterate together")) {
    return {
      advice:
        "Two lists feeding one fanned-out bot came back different lengths. Snap both sides from a single list whose items carry everything the bot needs.",
      docs: "docs/fan-out.md",
    };
  }

  if (m.includes("429") || m.includes("rate limit") || m.includes("quota")) {
    return {
      advice:
        "The model provider is rate-limiting or out of quota. Wait a minute and run it again, or point NANOBOTS_LLM at a provider with more headroom.",
      docs: "docs/llm.md",
    };
  }

  return null;
}

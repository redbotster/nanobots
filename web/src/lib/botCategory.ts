import type { BotSummary } from "./types";

/** Groups a bot by the service it primarily talks to — shared by the
 * builder's palette and the Bot Library so both categorize bots the same
 * way. */
export function categoryOf(bot: BotSummary): string {
  const providers = new Set(bot.services.map((s) => s.provider));
  if (providers.size === 0) return "Utility";
  if (providers.has("google")) return "Google";
  if (providers.has("github")) return "GitHub";
  return [...providers].map((p) => p[0].toUpperCase() + p.slice(1)).join(" + ");
}

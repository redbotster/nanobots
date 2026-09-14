import type { BotSummary } from "./types";

/** A provider slug as a person would write it. */
export function prettyProvider(slug: string): string {
  const special: Record<string, string> = {
    github: "GitHub",
    linkedin: "LinkedIn",
    x: "X",
    hubspot: "HubSpot",
    gdrive: "Google Drive",
    google_business_profile: "Google Business Profile",
  };
  if (special[slug]) return special[slug];
  return slug
    .split(/[_-]/)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

/** Groups a bot by the service it primarily talks to — shared by the
 * builder's palette and the Bot Library so both categorize bots the same
 * way.
 *
 * The primary service is the one the bot declares *first*, which is how the
 * catalog already writes them. Two earlier rules are gone:
 *
 * - Every distinct combination used to become its own category, so
 *   `lead-router` (hubspot, slack) sat alone under "Hubspot + Slack" while
 *   `lead-enricher` (hubspot) sat alone under "Hubspot". Four of seven
 *   categories held exactly one bot, and each one cost a heading plus a
 *   three-column grid row with two empty thirds.
 * - Google anywhere in the set won, so `invoice-chaser` (stripe, google) —
 *   a bot for chasing Stripe invoices — was filed under Google. First
 *   declared gets that right.
 */
export function categoryOf(bot: BotSummary): string {
  const first = bot.services[0]?.provider;
  if (!first) return "Utility";
  return prettyProvider(first);
}

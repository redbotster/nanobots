import type { SwarmSummary } from "./types";

/** Matches a swarm against what someone typed.
 *
 * Name and description, because people remember a swarm either way — "the
 * one that posts to Slack" is a description search, "get-paid" is a name
 * one. Case-insensitive, and every whitespace-separated term has to match
 * somewhere, so "slack digest" finds github-digest-to-slack without
 * depending on the order the words are remembered in.
 *
 * Here rather than beside the page that uses it so SwarmsPage exports only
 * components: Vite's fast refresh gives up on a module that mixes the two,
 * and a pure predicate over a type from this directory belongs here anyway.
 */
export function swarmMatches(s: SwarmSummary, query: string): boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return true;
  const hay = `${s.name} ${s.description ?? ""}`.toLowerCase();
  return terms.every((t) => hay.includes(t));
}

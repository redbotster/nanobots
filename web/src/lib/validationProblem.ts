import type { PlanResult } from "./types";

/** What's actually wrong with a !ok validation that has no top-level
 * `error` — a snap connection, an unfed required input, or a swarm-level
 * mistake belonging to neither. All three come back from the same
 * endpoint; BuilderPage's status line used to read only the first, so a
 * swarm whose real problem was an unconnected required input (no snap
 * involved at all) showed "0 connection(s) need fixing" — amber, and
 * false. Combined because more than one kind can be true at once; a bare
 * fallback because !ok with all three empty would otherwise silently
 * claim a number that isn't there either.
 *
 * Its own file, not inline in BuilderPage.tsx, so it can be unit tested
 * directly — the same reason ./builder/hydrate.ts is split out. */
export function describeValidationProblem(validation: PlanResult): string {
  const brokenSnaps = validation.snaps.filter((s) => !s.OK).length;
  const unfed = validation.unfed?.length ?? 0;
  const invalid = validation.invalid?.length ?? 0;

  const parts: string[] = [];
  if (brokenSnaps > 0) {
    parts.push(
      `${brokenSnaps} connection${brokenSnaps === 1 ? "" : "s"} need${brokenSnaps === 1 ? "s" : ""} fixing`,
    );
  }
  if (unfed > 0) {
    parts.push(`${unfed} input${unfed === 1 ? "" : "s"} need${unfed === 1 ? "s" : ""} a value`);
  }
  if (invalid > 0) {
    parts.push(invalid === 1 ? "1 problem to fix" : `${invalid} problems to fix`);
  }
  return parts.length > 0 ? parts.join(", ") : "something isn't right yet";
}

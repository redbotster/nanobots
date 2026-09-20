import { describe, expect, it } from "vitest";
import { describeValidationProblem } from "../lib/validationProblem";
import type { PlanResult } from "../lib/types";

function planResult(over: Partial<PlanResult>): PlanResult {
  return {
    swarm: "s",
    bots: [],
    snaps: [],
    ok: false,
    ...over,
  };
}

// A live flow audit found this one: a swarm whose real problem is an
// unconnected required input (drive-save.file, nothing supplies it) has
// zero broken snaps — the input was never snapped at all — so the old
// fallback (which only ever counted validation.snaps) read "0
// connection(s) need fixing": amber, and false about there being zero of
// anything.
describe("describeValidationProblem", () => {
  it("names an unfed input, not a snap count, when that's the real problem", () => {
    const got = describeValidationProblem(
      planResult({
        unfed: [{ bot: "drive-save", port: "file", reason: "nothing supplies it" }],
      }),
    );
    expect(got).toBe("1 input needs a value");
    expect(got).not.toContain("0 connection");
  });

  it("still names a broken snap connection", () => {
    const got = describeValidationProblem(
      planResult({
        snaps: [
          { From: "a.out", To: "b.in", OK: false },
          { From: "b.out", To: "c.in", OK: true },
        ],
      }),
    );
    expect(got).toBe("1 connection needs fixing");
  });

  it("names every kind of problem at once, not just the first it checks", () => {
    const got = describeValidationProblem(
      planResult({
        snaps: [{ From: "a.out", To: "b.in", OK: false }],
        unfed: [{ bot: "drive-save", port: "file", reason: "nothing supplies it" }],
        invalid: ['mailer: on_error "retyr" is not a real value'],
      }),
    );
    expect(got).toContain("1 connection needs fixing");
    expect(got).toContain("1 input needs a value");
    expect(got).toContain("1 problem to fix");
  });

  it("says something honest rather than a fabricated zero when nothing is countable", () => {
    const got = describeValidationProblem(planResult({}));
    expect(got).toBe("something isn't right yet");
  });
});

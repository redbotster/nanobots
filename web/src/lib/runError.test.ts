import { describe, expect, it } from "vitest";
import { parseRunError, shortRunError } from "./runError";

describe("parseRunError", () => {
  // Verbatim from a real failed run on this machine.
  it("strips the transport chain off a real error", () => {
    const raw =
      'container exited 1: nanobot-agent: error: bot notify: step "send": ' +
      "callback /internal/steps/notify: 1Claw vault is locked: Passkey " +
      "verification required to access vault secrets. Unlock with your passkey and retry.\n";
    expect(parseRunError(raw)).toEqual({
      origin: "notify/send",
      message:
        "1Claw vault is locked: Passkey verification required to access vault secrets. " +
        "Unlock with your passkey and retry.",
    });
  });

  it("handles the doubled container prefix older runs still carry", () => {
    const raw =
      "container exited 1: container exited 1: nanobot-agent: error: " +
      'bot content-ideas: step "brainstorm": callback /internal/steps/ai_generate: ' +
      "shroud: chat failed (401)";
    const parts = parseRunError(raw)!;
    expect(parts.origin).toBe("content-ideas/brainstorm");
    expect(parts.message).toBe("shroud: chat failed (401)");
  });

  it("keeps an error that has no step origin", () => {
    const parts = parseRunError("swarm does not type-check: port mismatch")!;
    expect(parts.origin).toBe("");
    expect(parts.message).toBe("swarm does not type-check: port mismatch");
  });

  it("keeps only the first line of a multi-line error", () => {
    const parts = parseRunError("first line\nsecond line\nthird")!;
    expect(parts.message).toBe("first line");
  });

  // The stripping must never be able to empty out an error — a blank row
  // would be strictly worse than the noisy one it replaced.
  it("never returns an empty message for a non-empty error", () => {
    for (const raw of [
      "container exited 1:",
      'bot notify: step "send":',
      "nanobot-agent: error:",
      "x",
    ]) {
      const parts = parseRunError(raw)!;
      expect(parts.message.length).toBeGreaterThan(0);
    }
  });

  it("returns null for nothing", () => {
    expect(parseRunError(undefined)).toBeNull();
    expect(parseRunError(null)).toBeNull();
    expect(parseRunError("   ")).toBeNull();
  });
});

describe("shortRunError", () => {
  it("joins origin and message for a row", () => {
    const raw = 'container exited 1: bot notify: step "send": vault is locked';
    expect(shortRunError(raw)).toBe("notify/send — vault is locked");
  });

  it("omits the separator when there is no origin", () => {
    expect(shortRunError("docker daemon not reachable")).toBe("docker daemon not reachable");
  });
});

// The runRemedy suite that used to live here moved to
// internal/remedy/remedy_test.go with the table it tests — same cases, same
// real errors. It is Go now because the CLI needs the same answers, and a
// second copy of the rules in TypeScript is how the two would have diverged.

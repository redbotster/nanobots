import { describe, expect, it } from "vitest";
import { parseRunError, shortRunError, runRemedy } from "./runError";

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

describe("runRemedy", () => {
  // The failure six of fifteen catalog swarms actually hit.
  it("offers to connect the service a missing credential names", () => {
    const r = runRemedy(
      'container exited 1: nanobot-agent: error: bot notify: step "send": callback ' +
        '/internal/steps/notify: no connected account yet (oneclaw: request failed (404): ' +
        '{"detail":"Secret slack/bot_token not found"}) — connect it from Settings',
    );
    expect(r?.action?.label).toBe("Connect Slack");
    expect(r?.action?.page).toBe("settings");
    expect(r?.advice).toContain("Slack");
  });

  it("names the right service, not just any service", () => {
    const stripe = runRemedy('bot invoice-chaser: Secret stripe/api_key not found');
    expect(stripe?.action?.label).toBe("Connect Stripe");
    const gh = runRemedy('bot github-issues-digest: Secret github/token not found');
    expect(gh?.action?.label).toBe("Connect GitHub");
  });

  // The vault fix is on a phone, so offering a button would be a lie about
  // where the work happens.
  it("explains the vault lock without pretending there is a button", () => {
    const r = runRemedy("1Claw vault is locked: Passkey verification required to access vault secrets.");
    expect(r?.advice).toContain("passkey");
    expect(r?.action).toBeUndefined();
  });

  // A declined approval is a decision. Someone debugging their own "no" is
  // the failure mode here.
  it("says a declined approval was not a fault", () => {
    const r = runRemedy('bot email-send-approved: step "gate": not approved (decided_by=cli)');
    expect(r?.advice).toContain("wasn't a fault");
  });

  it("distinguishes an unattended decline from a human one", () => {
    const r = runRemedy(
      'step "gate": not approved (decided_by=nobody — no terminal attached to ask)',
    );
    expect(r?.advice).toContain("Nothing was attached");
  });

  it("points a fan-out length mismatch at the doc that explains it", () => {
    const r = runRemedy(
      "fan-out inputs disagree: triage.tickets.*.subject has 2 item(s) but an earlier one has 1 — they iterate together, so they must be the same length",
    );
    expect(r?.docs).toBe("docs/fan-out.md");
  });

  it("recognises a provider rate limit", () => {
    const r = runRemedy(
      "llm: https://generativelanguage.googleapis.com/... returned 429: quota exceeded",
    );
    expect(r?.advice).toContain("rate-limiting");
  });

  // A confidently wrong suggestion sends someone to reconfigure a thing
  // that was never the problem, which is worse than saying nothing.
  it("offers nothing for a failure it does not recognise", () => {
    expect(runRemedy("bot repurposer: model response was not valid JSON")).toBeNull();
    expect(runRemedy("something nobody has seen before")).toBeNull();
    expect(runRemedy("")).toBeNull();
    expect(runRemedy(null)).toBeNull();
  });
});

describe("an unanswered approval", () => {
  it("is named as nobody being there, not as a hung bot", () => {
    // The most common failure on a machine running scheduled swarms: 54
    // runs on the development machine, every one an approval nobody
    // answered, all previously reported as "container exceeded 30m0s".
    const r = runRemedy(
      "nobody answered the approval \"Send 'recap.pdf' to me@example.com?\" after 30m0s, " +
        "so the bot was stopped — approve it from the Runs page while it's waiting, " +
        "or take the approval off this step if it shouldn't need one",
    );
    expect(r).not.toBeNull();
    expect(r!.advice).toContain("nobody answered in time");
    // "Run it again" is the wrong advice here — the next scheduled run goes
    // unanswered the same way. The advice must name a durable fix.
    expect(r!.advice).not.toContain("Run it again");
    expect(r!.advice).toMatch(/phone|approval off this step/);
  });

  it("is not confused with a decline, which is a decision", () => {
    const declined = runRemedy("bot mailer: step approve: not approved");
    expect(declined!.advice).toContain("declined");
    expect(declined!.advice).not.toContain("nobody answered in time");
  });
});

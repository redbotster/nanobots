import { describe, expect, it } from "vitest";
import { categoryOf } from "./botCategory";
import type { BotSummary } from "./types";

function bot(providers: string[]): BotSummary {
  return {
    id: "probe",
    name: "probe",
    version: "0.1.0",
    description: "",
    tags: [],
    harness: "bare",
    services: providers.map((provider, i) => ({ id: `s${i}`, provider })),
    inputs: [],
    outputs: [],
    guardrails: {},
  } as BotSummary;
}

describe("categoryOf", () => {
  it("files a bot under the service it declares first", () => {
    // invoice-chaser is (stripe, google) — a bot for chasing Stripe
    // invoices. The old rule was "google anywhere wins", which filed it
    // under Google.
    expect(categoryOf(bot(["stripe", "google"]))).toBe("Stripe");
    expect(categoryOf(bot(["hubspot", "slack"]))).toBe("HubSpot");
    expect(categoryOf(bot(["x", "linkedin"]))).toBe("X");
  });

  it("does not invent a category per combination", () => {
    // lead-enricher (hubspot) and lead-router (hubspot, slack) used to land
    // in two different one-bot categories, each costing a heading and a
    // grid row with two empty thirds.
    expect(categoryOf(bot(["hubspot"]))).toBe(categoryOf(bot(["hubspot", "slack"])));
  });

  it("calls a bot with no service a Utility", () => {
    expect(categoryOf(bot([]))).toBe("Utility");
  });

  it("writes provider names the way a person would", () => {
    expect(categoryOf(bot(["github"]))).toBe("GitHub");
    expect(categoryOf(bot(["linkedin"]))).toBe("LinkedIn");
    expect(categoryOf(bot(["google_business_profile"]))).toBe("Google Business Profile");
    // An unknown provider still gets title-cased rather than shown raw.
    expect(categoryOf(bot(["acme_crm"]))).toBe("Acme Crm");
  });
});

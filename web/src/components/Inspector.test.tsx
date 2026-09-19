import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Inspector } from "./Inspector";
import { api } from "../lib/api";
import type { BotSummary, StatusResponse } from "../lib/types";

const bot: BotSummary = {
  id: "inbox-triage",
  name: "inbox-triage",
  version: "0.1.0",
  description: "Sort new mail.",
  tags: [],
  harness: "llm",
  services: [],
  inputs: [],
  outputs: [],
  guardrails: { pii: "redact", injection_threshold: 0.7 },
};

function withBackend(llm: string, guarded: boolean) {
  vi.spyOn(api, "status").mockResolvedValue({
    oneclaw_configured: true,
    vault_locked: false,
    vault_reason: "",
    memory_backend: "local",
    memory_recall: false,
    memory_recall_bots: [],
    llm_backend: llm,
    llm_guardrails: guarded,
    docker_available: true,
    docker_reason: "",
    secrets_backend: "a local file, encrypted at rest",
  } as StatusResponse);
}

afterEach(() => vi.restoreAllMocks());

describe("the bot Inspector's guardrails panel", () => {
  it("says 1Claw enforces them when it does", async () => {
    withBackend("1claw shroud (token billing)", true);
    render(<Inspector bot={bot} />);
    await waitFor(() => expect(screen.getByText(/enforced by 1Claw/)).toBeTruthy());
    expect(screen.queryByText(/nothing is applying it/)).toBeNull();
  });

  // PII redaction and injection screening are Shroud's, not the bot's. On a
  // direct provider key the prompt goes straight to the model and neither
  // happens — and this panel promised both regardless, under a heading that
  // said "enforced by 1Claw". A guardrail claimed and not applied is worse
  // than one never claimed.
  it("stops claiming enforcement on a direct provider", async () => {
    withBackend("gemini (direct)", false);
    render(<Inspector bot={bot} />);
    await waitFor(() => expect(screen.getByText(/declared by this bot/)).toBeTruthy());
    expect(screen.queryByText(/enforced by 1Claw/)).toBeNull();
    // And says where the prompts actually go.
    expect(screen.getByText(/straight to gemini \(direct\)/)).toBeTruthy();
    expect(screen.getAllByText(/nothing is applying it/).length).toBe(2);
  });

  // Approvals and max runtime are this runner's, not Shroud's — they hold
  // whatever the model backend is, and must not be swept up in the warning.
  it("keeps claiming the guardrails the runner really does enforce", async () => {
    withBackend("gemini (direct)", false);
    render(
      <Inspector
        bot={{ ...bot, guardrails: { ...bot.guardrails, approval_required_for: ["email.send"] } }}
      />,
    );
    await waitFor(() => expect(screen.getByText(/enforced by the runner/)).toBeTruthy());
  });
});

import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsPage } from "./SettingsPage";
import { api } from "../lib/api";
import * as botsCache from "../lib/botsCache";
import type { StatusResponse } from "../lib/types";

afterEach(() => vi.restoreAllMocks());

function status(over: Partial<StatusResponse>): StatusResponse {
  return {
    oneclaw_configured: false,
    vault_locked: false,
    vault_reason: "",
    memory_backend: "local",
    memory_recall: false,
    memory_recall_bots: [],
    llm_backend: "gemini (direct)",
    llm_guardrails: false,
    docker_available: true,
    docker_reason: "",
    secrets_backend: "a local file, encrypted at rest — see docs/secrets.md",
    ...over,
  };
}

// The bug: the whole "Connect a service" block — the only UI for pasting a
// GitHub/Slack/Stripe/HubSpot token — was gated on oneclaw_configured, so a
// machine using the encrypted-file secrets backend (internal/secrets,
// docs/secrets.md — the exact case that needs no 1Claw account) had no way
// to connect any of the four static-token providers from Settings at all.
describe("Connect a service without 1Claw configured", () => {
  it("still offers the pasted-token providers", async () => {
    vi.spyOn(botsCache, "listBotsCached").mockResolvedValue([]);
    vi.spyOn(api, "listConnections").mockResolvedValue([]);

    render(<SettingsPage status={status({})} />);

    await waitFor(() => expect(screen.getByText("Connect a service")).toBeTruthy());
    expect(screen.getByText("Slack")).toBeTruthy();
    expect(screen.getByText("GitHub")).toBeTruthy();
    expect(screen.getByText("Stripe")).toBeTruthy();
    expect(screen.getByText("HubSpot")).toBeTruthy();
  });

  it("does not offer the OAuth providers, which still need 1Claw", async () => {
    vi.spyOn(botsCache, "listBotsCached").mockResolvedValue([]);
    vi.spyOn(api, "listConnections").mockResolvedValue([]);

    render(<SettingsPage status={status({})} />);

    await waitFor(() => expect(screen.getByText("Connect a service")).toBeTruthy());
    expect(screen.queryByText("Google")).toBeNull();
    expect(screen.queryByText("LinkedIn")).toBeNull();
  });

  it("names the active secrets backend in the System section", async () => {
    vi.spyOn(botsCache, "listBotsCached").mockResolvedValue([]);
    vi.spyOn(api, "listConnections").mockResolvedValue([]);

    render(
      <SettingsPage
        status={status({ secrets_backend: "your OS keychain — see docs/secrets.md" })}
      />,
    );

    await waitFor(() => expect(screen.getByText("Secrets")).toBeTruthy());
    expect(screen.getAllByText(/your OS keychain/).length).toBeGreaterThan(0);
  });
});

describe("Connect a service with 1Claw configured", () => {
  it("offers the OAuth providers alongside the pasted-token ones", async () => {
    vi.spyOn(botsCache, "listBotsCached").mockResolvedValue([]);
    vi.spyOn(api, "listConnections").mockResolvedValue([]);

    render(<SettingsPage status={status({ oneclaw_configured: true })} />);

    await waitFor(() => expect(screen.getByText("Connect a service")).toBeTruthy());
    expect(screen.getByText("Google")).toBeTruthy();
    expect(screen.getByText("GitHub")).toBeTruthy();
  });
});

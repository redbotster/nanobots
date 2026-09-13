import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WebhookPanel } from "./WebhookPanel";
import { api } from "../lib/api";
import type { SwarmSummary } from "../lib/types";

const swarm: SwarmSummary = {
  path: "examples/swarms/lead-to-meeting.yaml",
  name: "lead-to-meeting",
  description: "Route a new lead.",
  services_live: 0,
  services_total: 2,
  trigger_type: "webhook",
};

afterEach(() => vi.restoreAllMocks());

describe("WebhookPanel", () => {
  it("does not fetch the token until it is asked for", () => {
    const spy = vi.spyOn(api, "webhookDetails");
    render(<WebhookPanel swarm={swarm} />);

    // The panel says what the swarm does without pulling a credential into
    // the page. Every open tab polls the swarm list; the token should not
    // ride along with it.
    expect(screen.getByText(/Runs when something posts to it/)).toBeTruthy();
    expect(spy).not.toHaveBeenCalled();
  });

  it("gives you the URL, and keeps the token masked until you ask", async () => {
    vi.spyOn(api, "webhookDetails").mockResolvedValue({
      swarm: "lead-to-meeting",
      url: "http://localhost:8848/webhooks/lead-to-meeting",
      token: "s3cret-token-value",
      curl: "curl -X POST http://localhost:8848/webhooks/lead-to-meeting",
    });
    render(<WebhookPanel swarm={swarm} />);

    fireEvent.click(screen.getByRole("button", { name: /Show the URL/ }));

    // The URL is the thing you paste into a form service, so it is plain.
    await waitFor(() =>
      expect(screen.getByText("http://localhost:8848/webhooks/lead-to-meeting")).toBeTruthy(),
    );
    // The token is the thing you paste into a password field, so it is not.
    expect(screen.queryByText("s3cret-token-value")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "show" }));
    expect(screen.getByText("s3cret-token-value")).toBeTruthy();
  });

  it("says so when the daemon refuses, instead of showing an empty box", async () => {
    vi.spyOn(api, "webhookDetails").mockRejectedValue(
      new Error("webhook triggers are not enabled on this daemon"),
    );
    render(<WebhookPanel swarm={swarm} />);

    fireEvent.click(screen.getByRole("button", { name: /Show the URL/ }));
    await waitFor(() => expect(screen.getByText(/not enabled on this daemon/)).toBeTruthy());
  });
});

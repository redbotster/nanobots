import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GettingStarted } from "./GettingStarted";
import { api } from "../lib/api";
import * as runsFeed from "../lib/runsFeed";
import type { StatusResponse } from "../lib/types";

const status = (llm: string): StatusResponse =>
  ({
    oneclaw_configured: true,
    vault_locked: false,
    vault_reason: "",
    memory_backend: "local",
    memory_recall: false,
    memory_recall_bots: [],
    llm_backend: llm,
    llm_guardrails: false,
    docker_available: true,
    docker_reason: "",
  }) as StatusResponse;

function stub(runs: number, anyConnected: boolean) {
  // Through the shared feed, which is where this card reads the run list
  // now — a fetch of its own cost the whole 81KB list on every mount to
  // answer one boolean.
  vi.spyOn(runsFeed, "useRuns").mockReturnValue(
    Array.from({ length: runs }, (_, i) => ({ id: String(i) })) as never,
  );
  vi.spyOn(api, "listConnections").mockResolvedValue([
    { service: "slack", connected: anyConnected },
  ] as never);
}

beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

describe("GettingStarted", () => {
  it("shows what is left to do on a fresh install", async () => {
    stub(0, false);
    render(<GettingStarted status={status("none")} onOpenSettings={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("Getting started")).toBeTruthy());
    expect(screen.getByText(/Point it at a model/)).toBeTruthy();
    expect(screen.getByText(/Run one on example data/)).toBeTruthy();
    expect(screen.getByText(/Connect an account/)).toBeTruthy();
  });

  // Each step ticks itself off from real state, not from having clicked
  // something — a checklist you can complete without doing the thing is
  // worse than no checklist.
  it("ticks off a step that is genuinely done, and hides its blurb", async () => {
    stub(3, false);
    render(<GettingStarted status={status("gemini (direct)")} onOpenSettings={vi.fn()} />);
    // Partly done, so it starts collapsed — open it to check the steps.
    await waitFor(() => expect(screen.getByText("Show")).toBeTruthy());
    fireEvent.click(screen.getByText("Show"));
    // The model and run steps are done, so only the connect blurb remains.
    expect(screen.queryByText(/Without one, every bot returns/)).toBeNull();
    expect(screen.getByText(/Then the same swarm reads your actual inbox/)).toBeTruthy();
  });

  // It used to sit at the top of the landing page at full height whatever
  // your state, so the first swarm card started at y=533 of a 720px
  // viewport — three quarters of the screen spent explaining all three
  // steps to someone who had done two of them.
  it("collapses to one line once you have made a start", async () => {
    stub(3, false);
    render(<GettingStarted status={status("gemini (direct)")} onOpenSettings={vi.fn()} />);
    await waitFor(() => expect(screen.getByText(/2 of 3 done/)).toBeTruthy());
    // Names what is left, and nothing else.
    expect(screen.getByText(/Connect an account/)).toBeTruthy();
    expect(screen.queryByText(/Nothing here needs setting up to try/)).toBeNull();
    expect(screen.queryByText(/Then the same swarm reads your actual inbox/)).toBeNull();
  });

  // A genuinely new install is the case this card was written for, and the
  // one where the detail earns its space.
  it("stays full-height when nothing has been done yet", async () => {
    stub(0, false);
    render(<GettingStarted status={status("none")} onOpenSettings={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("Getting started")).toBeTruthy());
    expect(screen.getByText(/Nothing here needs setting up to try/)).toBeTruthy();
    expect(screen.queryByText("Show")).toBeNull();
  });

  // It has to get out of the way on its own. Someone who set everything up
  // should never see it, and should never have had to dismiss it.
  it("disappears entirely once all three are done", async () => {
    stub(1, true);
    const { container } = render(
      <GettingStarted status={status("1claw shroud (token billing)")} onOpenSettings={vi.fn()} />,
    );
    await waitFor(() => expect(container.textContent).not.toContain("Getting started"));
  });

  // And someone who knows what they're doing shouldn't have to finish a
  // tutorial to make it go away.
  it("can be dismissed, and stays dismissed", async () => {
    stub(0, false);
    const { unmount } = render(<GettingStarted status={status("none")} onOpenSettings={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("Getting started")).toBeTruthy());
    fireEvent.click(screen.getByText("Hide"));
    expect(screen.queryByText("Getting started")).toBeNull();

    unmount();
    render(<GettingStarted status={status("none")} onOpenSettings={vi.fn()} />);
    expect(screen.queryByText("Getting started")).toBeNull();
  });

  // A card that appeared because a request failed would be worse than one
  // that never appeared at all.
  it("stays hidden when it cannot tell what the state is", async () => {
    vi.spyOn(api, "listRuns").mockRejectedValue(new Error("down"));
    vi.spyOn(api, "listConnections").mockRejectedValue(new Error("down"));
    const { container } = render(<GettingStarted status={null} onOpenSettings={vi.fn()} />);
    await waitFor(() => expect(container.textContent).not.toContain("Getting started"));
  });
});

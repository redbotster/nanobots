import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LabPage } from "./LabPage";
import { api } from "../lib/api";
import * as apiModule from "../lib/api";
import type { LogEntry } from "../lib/types";

function stub() {
  // subscribeLabEvents opens a real EventSource, which jsdom doesn't
  // implement — stub it the same way GettingStarted's tests stub useRuns,
  // rather than let the real hook throw.
  vi.spyOn(apiModule, "subscribeLabEvents").mockReturnValue(() => {});
  return vi.spyOn(api, "sendLabMessage").mockResolvedValue({ ok: true });
}

afterEach(() => vi.restoreAllMocks());

describe("Lab's empty state", () => {
  // A blank chat with just a placeholder is the one screen in this app
  // that gives a first-time user nothing to click — every other page has a
  // gallery, a catalog, or a getting-started checklist. These chips are
  // that for Lab: real, on-brand examples (docs/team.md's own
  // `nanobots team run` case among them) rather than a bare text box.
  it("offers starter prompts", () => {
    stub();
    render(<LabPage />);
    for (const prompt of [
      "What can you help me with?",
      "Add a bot that watches Stripe for failed payments and posts to Slack",
    ]) {
      expect(screen.getByRole("button", { name: prompt })).toBeTruthy();
    }
  });

  it("sends the exact prompt text when a starter is clicked", async () => {
    const sendLabMessage = stub();
    render(<LabPage />);

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Explain what the morning-brief swarm does" }),
      );
    });

    expect(sendLabMessage).toHaveBeenCalledWith("Explain what the morning-brief swarm does");
  });

  it("hides the starters once a real conversation exists", async () => {
    const onEntry: { current: ((e: LogEntry) => void) | null } = { current: null };
    vi.spyOn(apiModule, "subscribeLabEvents").mockImplementation((cb) => {
      onEntry.current = cb;
      return () => {};
    });
    vi.spyOn(api, "sendLabMessage").mockResolvedValue({ ok: true });

    render(<LabPage />);
    expect(screen.queryByRole("button", { name: "What can you help me with?" })).toBeTruthy();

    act(() => onEntry.current?.({ time: new Date().toISOString(), bot: "you", msg: "hi" }));

    expect(screen.queryByRole("button", { name: "What can you help me with?" })).toBeNull();
  });
});

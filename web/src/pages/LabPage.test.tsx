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

// GET /api/lab/events replays the whole history before this tab ever calls
// send() — busy used to start false and only ever flip true from this
// tab's own send(), so opening (or reloading) mid-request replayed a
// transcript that plainly was not finished while showing an idle input
// with nothing on screen saying so.
describe("opening Lab mid-request", () => {
  it("shows busy from a replayed transcript, not just this tab's own send", () => {
    const onEntry: { current: ((e: LogEntry) => void) | null } = { current: null };
    vi.spyOn(apiModule, "subscribeLabEvents").mockImplementation((cb) => {
      onEntry.current = cb;
      return () => {};
    });
    vi.spyOn(api, "sendLabMessage").mockResolvedValue({ ok: true });

    render(<LabPage />);
    // Replay of a request that was still running when the tab connected:
    // the human's message and the delegation, but no final "lab" answer.
    act(() => {
      onEntry.current?.({ time: new Date().toISOString(), bot: "you", msg: "Build an example" });
      onEntry.current?.({
        time: new Date().toISOString(),
        bot: "lab",
        step: "delegating",
        msg: "delegating to designer: ...",
      });
      onEntry.current?.({
        time: new Date().toISOString(),
        bot: "team/designer",
        step: "tool",
        msg: "read_file CLAUDE.md",
      });
    });

    expect(screen.getByText("thinking")).toBeTruthy();
    expect(screen.getByPlaceholderText("Waiting on Lab's answer…")).toBeTruthy();

    // The real, final answer arrives — busy clears same as always.
    act(() =>
      onEntry.current?.({
        time: new Date().toISOString(),
        bot: "lab",
        msg: "Done. See bots/example.",
      }),
    );
    expect(screen.queryByText("thinking")).toBeNull();
  });
});

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TuneAnother } from "./TeamPage";
import { api } from "../lib/api";
import * as bots from "../lib/botsCache";
import type { BotSummary } from "../lib/types";

function bot(id: string, instructions: string): BotSummary {
  return {
    id,
    name: id,
    version: "0.1.0",
    description: "d",
    tags: [],
    harness: "llm",
    services: [],
    inputs: [{ name: "instructions", type: "string", default: instructions }],
    outputs: [],
    guardrails: {},
  } as unknown as BotSummary;
}

afterEach(() => vi.restoreAllMocks());

describe("adding a bot to the team", () => {
  // The bug: picking a bot POSTed its own shipped instructions straight
  // back, unchanged. The server records a bot only when the text actually
  // differs from what is already there, so nothing was recorded — the API
  // returned 200, the picker closed, and the page still said "Nothing tuned
  // yet". Picking a bot did nothing at all, silently.
  it("opens the bot's instructions instead of saving them unchanged", async () => {
    vi.spyOn(bots, "listBotsCached").mockResolvedValue([
      bot("inbox-triage", "Billing is urgent."),
    ] as never);
    const save = vi.spyOn(api, "setBotInstructions").mockResolvedValue({} as never);

    render(<TuneAnother tuned={new Set()} onChanged={vi.fn()} />);
    fireEvent.click(screen.getByText("+ Tune how a bot works"));
    await waitFor(() => expect(screen.getByText("inbox-triage")).toBeTruthy());
    fireEvent.click(screen.getByText("inbox-triage"));

    // An editor, seeded with what the bot ships with.
    const box = await screen.findByRole("textbox");
    expect((box as HTMLTextAreaElement).value).toBe("Billing is urgent.");
    // And nothing written yet — the save is the user's to make.
    expect(save).not.toHaveBeenCalled();
  });

  it("saves what you actually changed, which is what puts it in the team", async () => {
    vi.spyOn(bots, "listBotsCached").mockResolvedValue([
      bot("inbox-triage", "Billing is urgent."),
    ] as never);
    const save = vi.spyOn(api, "setBotInstructions").mockResolvedValue({} as never);
    const onChanged = vi.fn();

    render(<TuneAnother tuned={new Set()} onChanged={onChanged} />);
    fireEvent.click(screen.getByText("+ Tune how a bot works"));
    await waitFor(() => expect(screen.getByText("inbox-triage")).toBeTruthy());
    fireEvent.click(screen.getByText("inbox-triage"));

    const box = await screen.findByRole("textbox");
    fireEvent.change(box, { target: { value: "Refunds are urgent too." } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() =>
      expect(save).toHaveBeenCalledWith("inbox-triage", "Refunds are urgent too."),
    );
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  // Every bot has guardrails to tune, even one with no instructions port
  // at all (a bare-harness bot with no ai.generate step) — this used to
  // filter to instructions-taking bots only, which made a bot like
  // drive-save impossible to bring into the team for its guardrails.
  it("lists every untuned bot, including ones with no instructions port", async () => {
    const plain = { ...bot("drive-save", ""), inputs: [] } as unknown as BotSummary;
    vi.spyOn(bots, "listBotsCached").mockResolvedValue([
      bot("inbox-triage", "a"),
      bot("draft-replies", "b"),
      plain,
    ] as never);

    render(<TuneAnother tuned={new Set(["draft-replies"])} onChanged={vi.fn()} />);
    fireEvent.click(screen.getByText("+ Tune how a bot works"));

    await waitFor(() => expect(screen.getByText("inbox-triage")).toBeTruthy());
    // Already tuned — excluded.
    expect(screen.queryByText("draft-replies")).toBeNull();
    // No instructions port, but still tunable for its guardrails.
    expect(screen.getByText("drive-save")).toBeTruthy();
  });

  it("only offers an instructions editor for a bot that has that port", async () => {
    const plain = { ...bot("drive-save", ""), inputs: [] } as unknown as BotSummary;
    vi.spyOn(bots, "listBotsCached").mockResolvedValue([plain] as never);

    render(<TuneAnother tuned={new Set()} onChanged={vi.fn()} />);
    fireEvent.click(screen.getByText("+ Tune how a bot works"));
    await waitFor(() => expect(screen.getByText("drive-save")).toBeTruthy());
    fireEvent.click(screen.getByText("drive-save"));

    // No instructions box, but the guardrails editor opens straight away
    // since it's the only thing there is to tune.
    expect(screen.queryByText(/how this bot should work/i)).toBeNull();
    await waitFor(() => expect(screen.getByText(/what this bot is allowed to do/i)).toBeTruthy());
  });
});

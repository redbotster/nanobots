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

  it("lists only bots that take instructions, and not ones already tuned", async () => {
    const plain = { ...bot("drive-save", ""), inputs: [] } as unknown as BotSummary;
    vi.spyOn(bots, "listBotsCached").mockResolvedValue([
      bot("inbox-triage", "a"),
      bot("draft-replies", "b"),
      plain,
    ] as never);

    render(<TuneAnother tuned={new Set(["draft-replies"])} onChanged={vi.fn()} />);
    fireEvent.click(screen.getByText("+ Tune how a bot works"));

    await waitFor(() => expect(screen.getByText("inbox-triage")).toBeTruthy());
    expect(screen.queryByText("draft-replies")).toBeNull();
    expect(screen.queryByText("drive-save")).toBeNull();
  });
});

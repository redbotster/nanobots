import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { BuilderInspector } from "./BuilderInspector";
import type { CanvasSnap, PlacedBot } from "./BuilderCanvas";
import type { BotSummary } from "../../lib/types";

const def: BotSummary = {
  id: "notify",
  name: "notify",
  version: "0.1.0",
  description: "",
  tags: [],
  harness: "bare",
  services: [],
  inputs: [
    { name: "message", type: "string", required: true },
    { name: "channel", type: "string", required: true },
  ],
  outputs: [{ name: "delivered", type: "boolean" }],
  guardrails: {},
};

const placed: PlacedBot = { instanceId: "notifier", botId: "notify", x: 0, y: 0 };

function renderInspector(over: Partial<Parameters<typeof BuilderInspector>[0]> = {}) {
  const props = {
    bot: placed,
    def,
    snaps: [] as CanvasSnap[],
    values: {},
    onChange: vi.fn(),
    onEditSnapFrom: vi.fn(),
    onEditSnapJoin: vi.fn(),
    onEditOnError: vi.fn(),
    onClose: vi.fn(),
    ...over,
  };
  render(<BuilderInspector {...props} />);
  return props;
}

describe("BuilderInspector", () => {
  // `on_error` and `join` round-trip safely through the API and the YAML,
  // and the composer writes both — but until now there was no way to set
  // either by hand. The language was expressible everywhere except the
  // place built for expressing it.
  it("lets a bot be marked non-fatal, and defaults to stop", () => {
    const props = renderInspector();
    const select = screen.getByLabelText("If this bot fails") as HTMLSelectElement;
    expect(select.value).toBe("stop");

    fireEvent.change(select, { target: { value: "continue" } });
    expect(props.onEditOnError).toHaveBeenCalledWith("continue");
  });

  // Going back to the default sends "" rather than "stop", so the saved
  // YAML has no on_error key at all — the default should not become
  // clutter in every swarm file someone opens in the builder.
  it("clears the field rather than writing the default into the YAML", () => {
    const props = renderInspector({ bot: { ...placed, onError: "continue" } });
    const select = screen.getByLabelText("If this bot fails") as HTMLSelectElement;
    expect(select.value).toBe("continue");

    fireEvent.change(select, { target: { value: "stop" } });
    expect(props.onEditOnError).toHaveBeenCalledWith("");
  });

  // A join only means anything on a wired input, so it only appears there.
  it("offers no join on an unwired port", () => {
    renderInspector();
    expect(screen.queryByText("collapse a list")).toBeNull();
  });

  it("offers a join on a wired port", () => {
    renderInspector({ snaps: [{ from: "sender.acted_on", to: "notifier.message" }] });
    expect(screen.getByText("collapse a list")).toBeTruthy();
  });

  it("reports the chosen join against the right snap", () => {
    const props = renderInspector({
      snaps: [
        { from: "other.x", to: "somebody.else" },
        { from: "sender.acted_on", to: "notifier.message" },
      ],
    });
    const joinSelect = screen.getByText("collapse a list").parentElement!.querySelector("select")!;
    fireEvent.change(joinSelect, { target: { value: "lines" } });
    // Index 1 — the snap into this bot, not the unrelated one first in the list.
    expect(props.onEditSnapJoin).toHaveBeenCalledWith(1, "lines");
  });

  it("shows an existing join rather than resetting it to none", () => {
    renderInspector({
      snaps: [{ from: "sender.acted_on", to: "notifier.message", join: "count" }],
    });
    const joinSelect = screen.getByText("collapse a list").parentElement!.querySelector("select")!;
    expect((joinSelect as HTMLSelectElement).value).toBe("count");
  });

  // An accessibility audit found this one: the close button read to a
  // screen reader as a bare "✕" glyph, not which bot's inspector it closes.
  it("its close button has an accessible name, not just the glyph", () => {
    renderInspector();
    expect(screen.getByRole("button", { name: "Close notify's inspector" })).toBeInTheDocument();
  });
});

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Sheet } from "./Sheet";

// An accessibility audit found this one: the close button had neither an
// aria-label nor a title, the weakest of the four icon-only close buttons
// this session's audit found — a screen reader had nothing but a bare "✕"
// glyph to announce.
describe("Sheet", () => {
  it("its close button has an accessible name naming what it closes", () => {
    render(
      <Sheet open title="notify.yaml" onOpenChange={() => {}}>
        <p>content</p>
      </Sheet>,
    );
    expect(screen.getByRole("button", { name: "Close notify.yaml" })).toBeInTheDocument();
  });
});

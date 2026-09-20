import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PortBadge } from "./PortBadge";

function dotClass(container: HTMLElement) {
  return container.querySelector("span")!.className;
}

describe("PortBadge", () => {
  it("defaults to the unwired look when no state is given", () => {
    const { container } = render(<PortBadge name="channel" type="string" />);
    expect(dotClass(container)).toContain("border-muted");
  });

  it("renders wired distinctly from unwired", () => {
    const { container } = render(<PortBadge name="channel" type="string" state="wired" />);
    expect(dotClass(container)).toContain("border-tron");
  });

  // The one state worth standing out: a required input nothing feeds —
  // the run fails here the moment it starts.
  it("renders an unfed required input in the danger color, not just dim", () => {
    const { container } = render(<PortBadge name="file" type="string" state="unfed" />);
    expect(dotClass(container)).toContain("border-danger");
  });
});

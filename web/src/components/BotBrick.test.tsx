import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { BotBrick } from "./BotBrick";
import type { BotSummary } from "../lib/types";

const bot: BotSummary = {
  id: "recap-emails-to-pdf",
  name: "Recap inbox to PDF",
  version: "0.3.0",
  description: "Summarise unread mail into a PDF.",
  tags: [],
  harness: "openclaw",
  services: [{ id: "gmail", provider: "google", connection: "demo" }],
  inputs: [{ name: "since", type: "datetime" }],
  outputs: [{ name: "recap_pdf", type: "file" }],
  guardrails: {},
};

describe("BotBrick", () => {
  it("shows the bot's name, id, and services", () => {
    render(<BotBrick bot={bot} active={false} onClick={() => {}} />);
    expect(screen.getByText("Recap inbox to PDF")).toBeInTheDocument();
    expect(screen.getByText("recap-emails-to-pdf")).toBeInTheDocument();
    expect(screen.getByText("gmail")).toBeInTheDocument();
  });

  it("calls onClick when pressed", () => {
    const onClick = vi.fn();
    render(<BotBrick bot={bot} active={false} onClick={onClick} />);
    fireEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});

// Before wiringByInstance existed (SwarmView), every input dot was plain-
// muted and every output dot was plain-bright regardless of whether that
// *specific* port was fed by a real snap — a fixed styling convention, not
// a reflection of the swarm. These lock in that a port's dot now reflects
// its own wiring, not just which side of the card it's on.
const multiPortBot: BotSummary = {
  ...bot,
  inputs: [
    { name: "sheet", type: "string", required: true },
    { name: "question", type: "string" },
  ],
  outputs: [
    { name: "report_md", type: "string" },
    { name: "chart_png", type: "file" },
  ],
};

function portDots(container: HTMLElement, side: "-left-1.5" | "-right-1.5") {
  const wrap = container.querySelector(`[class*="${side}"]`)!;
  return [...wrap.querySelectorAll("span")];
}

describe("a bot's port dots reflect real per-port wiring", () => {
  it("marks only the actually-wired ports, not every port on that side", () => {
    const { container } = render(
      <BotBrick
        bot={multiPortBot}
        active={false}
        onClick={() => {}}
        wiredInputs={new Set(["sheet"])}
        wiredOutputs={new Set(["report_md"])}
      />,
    );
    const inputs = portDots(container, "-left-1.5");
    const outputs = portDots(container, "-right-1.5");
    expect(inputs[0].className).toContain("border-tron"); // sheet: wired
    expect(inputs[1].className).toContain("border-muted"); // question: not wired
    expect(outputs[0].className).toContain("border-tron"); // report_md: wired
    expect(outputs[1].className).toContain("border-muted"); // chart_png: not wired
  });

  it("marks a required, unfed input distinctly from a merely-unwired one", () => {
    const { container } = render(
      <BotBrick
        bot={multiPortBot}
        active={false}
        onClick={() => {}}
        unfedInputs={new Set(["sheet"])}
      />,
    );
    const [sheetDot, questionDot] = portDots(container, "-left-1.5");
    expect(sheetDot.className).toContain("border-danger");
    expect(questionDot.className).toContain("border-muted");
  });
});

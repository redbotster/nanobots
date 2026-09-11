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

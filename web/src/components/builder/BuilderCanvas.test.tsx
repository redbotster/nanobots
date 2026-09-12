import { render, screen, waitFor } from "@testing-library/react";
import { useEffect, useState } from "react";
import { describe, expect, it } from "vitest";
import { BuilderCanvas, type CanvasSnap, type PlacedBot } from "./BuilderCanvas";
import type { BotSummary } from "../../lib/types";

const bot = (id: string, inputs: string[], outputs: string[]): BotSummary => ({
  id,
  name: id,
  version: "0.1.0",
  description: "",
  tags: [],
  harness: "bare",
  services: [],
  inputs: inputs.map((name) => ({ name, type: "text" })),
  outputs: outputs.map((name) => ({ name, type: "text" })),
  guardrails: {},
});

const BOTS: PlacedBot[] = [
  { instanceId: "reporter", botId: "sheet-reporter", x: 0, y: 0 },
  { instanceId: "pdf", botId: "render-pdf", x: 300, y: 0 },
];
const SNAPS: CanvasSnap[] = [{ from: "reporter.report_md", to: "pdf.content" }];
const DEFS: Record<string, BotSummary> = {
  "sheet-reporter": bot("sheet-reporter", ["sheet"], ["report_md"]),
  "render-pdf": bot("render-pdf", ["content"], ["pdf"]),
};

function connectorCount(container: HTMLElement) {
  // The connector layer is the only <svg> the canvas itself owns.
  const svg = container.querySelector("svg");
  return svg ? svg.querySelectorAll("path").length : 0;
}

const noop = () => {};
const canvasProps = {
  snapChecks: [],
  onMoveBot: noop,
  onRemoveBot: noop,
  onAddSnap: noop,
  onRemoveSnap: noop,
  selectedInstanceId: null,
  onSelectBot: noop,
};

describe("BuilderCanvas connectors", () => {
  it("draws one connector per snap", () => {
    const { container } = render(
      <BuilderCanvas bots={BOTS} botDefs={DEFS} snaps={SNAPS} {...canvasProps} />,
    );
    expect(screen.getByText("reporter")).toBeInTheDocument();
    expect(connectorCount(container)).toBe(SNAPS.length);
  });

  // The regression this exists for: opening an existing swarm to edit it.
  // BuilderPage sets bots and snaps from GET /api/swarms/full, but the bot
  // *definitions* (which decide whether any port element renders at all)
  // arrive from a separate GET /api/bots. When they landed second, the
  // canvas had already measured an empty DOM, and nothing in [bots, snaps]
  // changed afterwards to make it measure again — so every connection in
  // the swarm was invisible.
  it("draws connectors when bot definitions arrive after the snaps", async () => {
    function LateDefs() {
      const [defs, setDefs] = useState<Record<string, BotSummary>>({});
      useEffect(() => {
        setDefs(DEFS); // the /api/bots response landing a tick later
      }, []);
      return <BuilderCanvas bots={BOTS} botDefs={defs} snaps={SNAPS} {...canvasProps} />;
    }

    const { container } = render(<LateDefs />);
    await waitFor(() => expect(screen.getByText("reporter")).toBeInTheDocument());

    expect(connectorCount(container)).toBe(SNAPS.length);
  });

  it("draws nothing for a snap whose port doesn't exist", () => {
    const { container } = render(
      <BuilderCanvas
        bots={BOTS}
        botDefs={DEFS}
        snaps={[{ from: "reporter.nope", to: "pdf.content" }]}
        {...canvasProps}
      />,
    );
    expect(connectorCount(container)).toBe(0);
  });
});

import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SwarmView } from "./SwarmView";
import * as useRunModule from "../lib/useRun";
import { api } from "../lib/api";
import type { SwarmSummary } from "../lib/types";

function swarm(over: Partial<SwarmSummary>): SwarmSummary {
  return {
    path: "examples/swarms/daily-email-recap.yaml",
    name: "daily-email-recap",
    description: "d",
    services_live: 0,
    services_total: 2,
    ...over,
  } as SwarmSummary;
}

function stubApi() {
  vi.spyOn(api, "plan").mockResolvedValue({
    swarm: "daily-email-recap",
    order: [],
    bots: [],
    snaps: [],
  } as never);
  vi.spyOn(api, "listBots").mockResolvedValue([] as never);
}

afterEach(() => vi.restoreAllMocks());

describe("opening a swarm", () => {
  // The dead end this fixes: the page only knew about runs started from it,
  // so it always said "Nothing's run yet" — including for a swarm whose run
  // was at that moment sitting on an approval. Following "waiting for your
  // approval" from the swarm list landed you on a page claiming nothing had
  // ever run, with no way to answer from there.
  it("watches the swarm's most recent run instead of starting blank", async () => {
    stubApi();
    const useRun = vi.spyOn(useRunModule, "useRun").mockReturnValue({
      run: null,
      isTerminal: false,
    } as never);

    render(
      <SwarmView
        swarm={swarm({ last_run_id: "run-abc", last_run_status: "awaiting_approval" })}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(useRun).toHaveBeenCalledWith("run-abc");
    });
  });

  it("still starts blank for a swarm that genuinely has never run", async () => {
    stubApi();
    const useRun = vi.spyOn(useRunModule, "useRun").mockReturnValue({
      run: null,
      isTerminal: false,
    } as never);

    render(<SwarmView swarm={swarm({})} onBack={() => {}} />);

    await waitFor(() => {
      expect(useRun).toHaveBeenCalledWith(null);
    });
    expect(screen.getByText(/nothing's run yet/i)).toBeTruthy();
  });
});

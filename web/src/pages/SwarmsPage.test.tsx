import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LastRunLine } from "./SwarmsPage";
import type { SwarmSummary } from "../lib/types";

function swarm(over: Partial<SwarmSummary>): SwarmSummary {
  return {
    path: "examples/swarms/content-engine.yaml",
    name: "content-engine",
    description: "d",
    services_live: 0,
    services_total: 2,
    ...over,
  } as SwarmSummary;
}

describe("the swarm card's run line", () => {
  // The failure this app actually has: 54 runs on the development machine
  // died at their container timeout with an approval nobody answered, 27
  // hours of container time. The whole time, the card for that swarm said
  // "last ran 2h ago" in muted grey — past tense about a run that had not
  // finished, and nothing to suggest a person was being waited on.
  it("says a swarm is waiting on you, not when it last ran", () => {
    render(
      <LastRunLine
        swarm={swarm({
          last_run_status: "awaiting_approval",
          last_run_at: "2026-09-15T10:00:00Z",
        })}
      />,
    );
    expect(screen.getByText(/waiting for your approval/i)).toBeTruthy();
    expect(screen.queryByText(/last ran/i)).toBeNull();
  });

  // Same bug, smaller stakes: a run in progress is not a run that happened.
  it("says a running swarm is running, not when it last ran", () => {
    render(
      <LastRunLine
        swarm={swarm({ last_run_status: "running", last_run_at: "2026-09-15T10:00:00Z" })}
      />,
    );
    expect(screen.getByText(/running now/i)).toBeTruthy();
    expect(screen.queryByText(/last ran/i)).toBeNull();
  });

  it("still says when a finished run last ran", () => {
    render(
      <LastRunLine
        swarm={swarm({
          last_run_status: "succeeded",
          last_run_at: new Date(Date.now() - 3600_000).toISOString(),
          last_run_trigger: "schedule",
        })}
      />,
    );
    expect(screen.getByText(/last ran/i)).toBeTruthy();
    expect(screen.getByText(/scheduled/i)).toBeTruthy();
  });

  it("says nothing has run yet when nothing has", () => {
    render(<LastRunLine swarm={swarm({})} />);
    expect(screen.getByText(/never run yet/i)).toBeTruthy();
  });
});

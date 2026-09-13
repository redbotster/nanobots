import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RunsPage } from "./RunsPage";
import type { RunSummary } from "../lib/types";
import * as runsFeed from "../lib/runsFeed";

function run(over: Partial<RunSummary> & { id: string }): RunSummary {
  return {
    swarm_name: "support-desk-lite",
    status: "failed",
    started_at: "2026-09-13T12:00:00Z",
    triggered_by: "schedule",
    ...over,
  } as RunSummary;
}

function withRuns(runs: RunSummary[]) {
  vi.spyOn(runsFeed, "useRuns").mockReturnValue(runs);
}

afterEach(() => vi.restoreAllMocks());

describe("RunsPage", () => {
  it("collapses a streak of identical failures into one row", () => {
    // The real case: one swarm failed 85 times because Slack was never
    // connected. Eighty-five rows reads as "everything is broken" and buries
    // the failures that were actually different.
    withRuns(
      Array.from({ length: 6 }, (_, i) =>
        run({
          id: `r${i}`,
          started_at: `2026-09-13T1${i}:00:00Z`,
          error: "slack is not connected",
        }),
      ),
    );
    render(<RunsPage />);

    expect(screen.getByText(/and 5 more that failed the same way/)).toBeTruthy();
    // One visible row, not six.
    expect(screen.getAllByText("support-desk-lite")).toHaveLength(1);

    fireEvent.click(screen.getByText(/and 5 more that failed the same way/));
    expect(screen.getAllByText("support-desk-lite")).toHaveLength(6);
  });

  it("does not collapse across a success", () => {
    // The list is a story in order. Merging failures that straddle a
    // success would claim a streak that never happened.
    withRuns([
      run({ id: "c", started_at: "2026-09-13T13:00:00Z", error: "boom" }),
      run({ id: "b", started_at: "2026-09-13T12:00:00Z", status: "succeeded", error: undefined }),
      run({ id: "a", started_at: "2026-09-13T11:00:00Z", error: "boom" }),
    ]);
    render(<RunsPage />);
    expect(screen.queryByText(/more that failed the same way/)).toBeNull();
  });

  it("does not collapse different failures, however similar the swarm", () => {
    withRuns([
      run({ id: "b", started_at: "2026-09-13T13:00:00Z", error: "slack is not connected" }),
      run({ id: "a", started_at: "2026-09-13T12:00:00Z", error: "container exceeded 30m0s" }),
    ]);
    render(<RunsPage />);
    expect(screen.queryByText(/more that failed the same way/)).toBeNull();
    expect(screen.getAllByText("support-desk-lite")).toHaveLength(2);
  });

  it("filters to the runs that need a person", () => {
    withRuns([
      run({ id: "a", error: "boom" }),
      run({ id: "b", status: "awaiting_approval", swarm_name: "get-paid", error: undefined }),
      run({ id: "c", status: "succeeded", swarm_name: "morning-brief", error: undefined }),
    ]);
    render(<RunsPage />);

    // A single waiting run opens straight into it rather than filtering, so
    // filter via the chip here.
    fireEvent.click(screen.getByRole("button", { name: /Needs you/ }));
    expect(screen.getByText("get-paid")).toBeTruthy();
    expect(screen.queryByText("morning-brief")).toBeNull();
  });
});

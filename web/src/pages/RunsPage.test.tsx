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

// A watch that finds nothing new produces a run an hour that correctly does
// nothing. Twenty-four rows saying "succeeded" bury the one row that acted
// just as effectively as a wall of red buried the two failures that were
// different — which is the problem the failure collapsing already solved.
describe("a run that found nothing to do", () => {
  it("says so instead of just succeeding, and collapses the repeats", async () => {
    withRuns([
      run({ id: "a", status: "succeeded", swarm_name: "meeting-to-action", error: undefined }),
      run({
        id: "b",
        status: "succeeded",
        swarm_name: "meeting-to-action",
        error: undefined,
        nothing_to_do: "no new file in this folder since the last run",
      }),
      run({
        id: "c",
        status: "succeeded",
        swarm_name: "meeting-to-action",
        error: undefined,
        nothing_to_do: "no new file in this folder since the last run",
      }),
    ]);
    render(<RunsPage />);

    expect(screen.getByText("nothing to do")).toBeTruthy();
    expect(screen.getByText(/no new file in this folder since the last run/)).toBeTruthy();
    expect(screen.getByText(/and 1 more with nothing to do/)).toBeTruthy();
    // The run that did work is not swallowed into the group.
    expect(screen.getByText("succeeded")).toBeTruthy();
  });
});

// Declining an approval is answering the question, not breaking anything.
//
// `declined_by_user` was set on the Run, persisted to history and already
// honoured by the scheduler's circuit breaker — it just never reached the
// wire, so this page rendered a run you said "no" to exactly like one that
// crashed: red dot, red error line, and counted in Failed. The first
// example in CLAUDE.md's list of honesty bugs, arriving through the one
// door that was left open.
describe("a run you declined", () => {
  const declined = run({
    id: "d1",
    swarm_name: "lead-to-meeting",
    declined_by_user: true,
    error: 'bot email-send-approved: step "gate": not approved (decided_by=you)',
  });

  it("does not say failed, and is not in the Failed count", () => {
    withRuns([
      declined,
      run({ id: "f1", swarm_name: "morning-brief", error: "docker is not running" }),
    ]);
    render(<RunsPage />);

    expect(screen.getByText("declined")).toBeTruthy();
    // One real failure beside it, so a count of 1 proves the declined run
    // was excluded rather than that nothing was counted.
    expect(screen.getByText("Failed").parentElement?.textContent).toContain("1");
  });

  it("is not listed under the Failed filter", () => {
    withRuns([
      declined,
      run({ id: "f1", swarm_name: "morning-brief", error: "docker is not running" }),
    ]);
    render(<RunsPage />);

    fireEvent.click(screen.getByText("Failed"));
    expect(screen.queryByText("lead-to-meeting")).toBeNull();
    expect(screen.getByText("morning-brief")).toBeTruthy();
  });

  // Hiding it entirely — what a stopped run does — would be worse here: a
  // schedule that keeps asking the same thing is worth seeing, and the
  // error is what names the step.
  it("still says which gate, just not in red", () => {
    withRuns([declined]);
    const { container } = render(<RunsPage />);

    const line = [...container.querySelectorAll("div")].find((d) =>
      d.textContent?.includes("not approved"),
    );
    expect(line).toBeTruthy();
    expect(line!.className).not.toContain("text-danger");
  });
});

import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RunsPage } from "./RunsPage";
import type { FoundryJob, RunSummary } from "../lib/types";
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

function foundryJob(over: Partial<FoundryJob> & { id: string }): FoundryJob {
  return {
    status: "awaiting_approval",
    started_at: "2026-09-13T12:00:00Z",
    request: "watch competitor pricing pages",
    missing_capability: "no bot reads a pricing page",
    iterations: 1,
    conform_ok: true,
    log: [],
    pending_approvals: [],
    ...over,
  } as FoundryJob;
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

  // Two watch bots on interleaved hourly crons never produce two
  // consecutive quiet runs from the same swarm — the other swarm's quiet
  // run always sits between them. Consecutive-only grouping collapsed
  // nothing here, which is the wall-of-noise problem this feature exists to
  // prevent, back again for a second swarm.
  it("collapses across an interleaved different swarm's own quiet runs", () => {
    withRuns([
      run({
        id: "a1",
        status: "succeeded",
        swarm_name: "meeting-to-action",
        error: undefined,
        started_at: "2026-09-13T14:00:00Z",
        nothing_to_do: "no new file in this folder since the last run",
      }),
      run({
        id: "b1",
        status: "succeeded",
        swarm_name: "repurpose-everything",
        error: undefined,
        started_at: "2026-09-13T13:30:00Z",
        nothing_to_do: "no new file in this folder since the last run",
      }),
      run({
        id: "a2",
        status: "succeeded",
        swarm_name: "meeting-to-action",
        error: undefined,
        started_at: "2026-09-13T13:00:00Z",
        nothing_to_do: "no new file in this folder since the last run",
      }),
      run({
        id: "b2",
        status: "succeeded",
        swarm_name: "repurpose-everything",
        error: undefined,
        started_at: "2026-09-13T12:30:00Z",
        nothing_to_do: "no new file in this folder since the last run",
      }),
    ]);
    render(<RunsPage />);

    // One visible row per swarm, not four.
    expect(screen.getAllByText("meeting-to-action")).toHaveLength(1);
    expect(screen.getAllByText("repurpose-everything")).toHaveLength(1);
    expect(screen.getAllByText(/and 1 more with nothing to do/)).toHaveLength(2);
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

// The gap the live UI audit found: get-paid's reminders went out, notify
// silently didn't, and the run listed as a plain "succeeded" — identical to
// one where nothing was skipped. tolerated_count is a count, not the
// failures themselves (RunDetail's own banner already renders those), but
// the list has to say *something* changed.
describe("a run that finished with a tolerated failure", () => {
  it("still says succeeded, but flags it and names the count", () => {
    withRuns([
      run({
        id: "t1",
        swarm_name: "get-paid",
        status: "succeeded",
        error: undefined,
        tolerated_count: 1,
      }),
    ]);
    const { container } = render(<RunsPage />);

    expect(screen.getByText("succeeded")).toBeTruthy();
    expect(screen.getByText(/1 step didn't run/i)).toBeTruthy();
    const dot = container.querySelector("span.inline-block");
    expect(dot?.className).toContain("bg-warn");
  });

  it("is still counted and listed under Succeeded, not Failed", () => {
    withRuns([
      run({
        id: "t1",
        swarm_name: "get-paid",
        status: "succeeded",
        error: undefined,
        tolerated_count: 1,
      }),
    ]);
    render(<RunsPage />);

    expect(screen.getByText("Succeeded").parentElement?.textContent).toContain("1");
    expect(screen.getByText("Failed").parentElement?.textContent).toContain("0");
    fireEvent.click(screen.getByText("Succeeded"));
    expect(screen.getByText("get-paid")).toBeTruthy();
  });

  it("plain green, no note, when nothing was tolerated", () => {
    withRuns([run({ id: "s1", status: "succeeded", error: undefined })]);
    const { container } = render(<RunsPage />);

    expect(screen.queryByText(/didn't run/i)).toBeNull();
    expect(container.querySelector("span.inline-block")?.className).toContain("bg-ok");
  });
});

// The nav badge counts swarm runs awaiting approval plus pending foundry
// jobs (see useApprovalNotifications), but this page used to have no idea
// foundry jobs existed at all — the badge said one number, this page
// silently showed a smaller one, and once you navigated away from the
// exact composer-gap screen that first opened a job there was nowhere left
// to click through to it. This is what closes that gap.
describe("a foundry job waiting on your review", () => {
  it("renders its own banner, distinct from the swarm-run one, naming the request", () => {
    withRuns([]);
    const onOpen = vi.fn();
    render(<RunsPage pendingFoundryJobs={[foundryJob({ id: "f1" })]} onOpenFoundryJob={onOpen} />);

    expect(screen.getByText(/needs your review/i)).toBeTruthy();
    expect(screen.getByText("watch competitor pricing pages")).toBeTruthy();
  });

  it("opens the job when clicked", () => {
    withRuns([]);
    const onOpen = vi.fn();
    render(<RunsPage pendingFoundryJobs={[foundryJob({ id: "f1" })]} onOpenFoundryJob={onOpen} />);

    fireEvent.click(screen.getByText(/needs your review/i));
    expect(onOpen).toHaveBeenCalledWith("f1");
  });

  it("renders one row per job when more than one is pending", () => {
    withRuns([]);
    render(
      <RunsPage
        pendingFoundryJobs={[
          foundryJob({ id: "f1", request: "watch competitor pricing pages" }),
          foundryJob({ id: "f2", request: "summarise support tickets weekly" }),
        ]}
        onOpenFoundryJob={vi.fn()}
      />,
    );

    expect(screen.getAllByText(/needs your review/i)).toHaveLength(2);
    expect(screen.getByText("summarise support tickets weekly")).toBeTruthy();
  });

  it("shows nothing when there is nothing pending", () => {
    withRuns([]);
    render(<RunsPage pendingFoundryJobs={[]} onOpenFoundryJob={vi.fn()} />);
    expect(screen.queryByText(/needs your review/i)).toBeNull();
  });
});

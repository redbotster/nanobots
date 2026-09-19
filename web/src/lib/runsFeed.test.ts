import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RunsEvent } from "./api";
import type { RunSummary } from "./types";

// One shared SSE connection, and exactly one — the poll this replaced ran
// two chains in lockstep when a subscriber churned mid-flight (React's
// StrictMode does exactly that on mount), so the same property matters
// here even though the transport changed.
//
// runsFeed keeps its connection state in module-level singletons, so each
// test resets the module registry and re-imports it fresh via loadFeed()
// below — without that, one test's snapshot data or open connection would
// leak into the next.

function run(
  id: string,
  startedAt: string,
  status: RunSummary["status"] = "succeeded",
): RunSummary {
  return {
    id,
    swarm_name: "probe",
    status,
    started_at: startedAt,
    triggered_by: "manual",
    swarm_path: "examples/swarms/probe.yaml",
    pending_approval_count: 0,
  };
}

/** Fresh copies of the api module (stubbed) and runsFeed (reset), plus a
 * way to push frames and count connection opens/closes. */
async function loadFeed() {
  vi.resetModules();
  const apiModule = await import("./api");
  const closes = vi.fn();
  const opens = vi.fn();
  let onEvent: ((ev: RunsEvent) => void) | null = null;
  vi.spyOn(apiModule, "subscribeRunsEvents").mockImplementation((fn) => {
    opens();
    onEvent = fn;
    return closes;
  });
  const { useRuns } = await import("./runsFeed");
  return {
    useRuns,
    opens,
    closes,
    push: (ev: RunsEvent) => act(() => onEvent?.(ev)),
  };
}

beforeEach(() => {
  Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
});
afterEach(() => vi.restoreAllMocks());

describe("the shared runs feed", () => {
  it("opens exactly one connection for two subscribers", async () => {
    const feed = await loadFeed();
    renderHook(() => feed.useRuns());
    renderHook(() => feed.useRuns());
    expect(feed.opens).toHaveBeenCalledTimes(1);
  });

  // React's StrictMode does subscribe/unsubscribe/subscribe on mount. An
  // EventSource's close() is synchronous and immediate — unlike the old
  // poll's in-flight fetch, there's no window where a second connection
  // can end up open beside the first.
  it("does not leave two connections open after a subscriber churns", async () => {
    const feed = await loadFeed();
    const first = renderHook(() => feed.useRuns());
    first.unmount();
    renderHook(() => feed.useRuns());
    expect(feed.opens).toHaveBeenCalledTimes(2); // closed, then reopened — never two live at once
    expect(feed.closes).toHaveBeenCalledTimes(1);
  });

  it("closes when the last subscriber goes away", async () => {
    const feed = await loadFeed();
    const view = renderHook(() => feed.useRuns());
    view.unmount();
    expect(feed.closes).toHaveBeenCalledTimes(1);
  });

  // nanobotd is a local process people leave running all day; an idle
  // background tab holding an open connection for a list nobody is
  // looking at is exactly the cost this replaced polling to avoid.
  it("closes when the tab is hidden and reconnects when it isn't", async () => {
    const feed = await loadFeed();
    renderHook(() => feed.useRuns());
    expect(feed.opens).toHaveBeenCalledTimes(1);

    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
    document.dispatchEvent(new Event("visibilitychange"));
    expect(feed.closes).toHaveBeenCalledTimes(1);

    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    document.dispatchEvent(new Event("visibilitychange"));
    expect(feed.opens).toHaveBeenCalledTimes(2);
  });

  it("is null until the first snapshot, then renders it newest first", async () => {
    const feed = await loadFeed();
    const view = renderHook(() => feed.useRuns());
    expect(view.result.current).toBeNull(); // "loading", not "genuinely empty"

    feed.push({
      type: "snapshot",
      runs: [run("a", "2026-01-01T00:00:02Z"), run("b", "2026-01-01T00:00:01Z")],
    });
    expect(view.result.current?.map((r) => r.id)).toEqual(["a", "b"]);
  });

  it("merges a delta into the existing row rather than appending it", async () => {
    const feed = await loadFeed();
    const view = renderHook(() => feed.useRuns());
    feed.push({
      type: "snapshot",
      runs: [run("a", "2026-01-01T00:00:02Z"), run("b", "2026-01-01T00:00:01Z")],
    });

    feed.push({ type: "run", run: run("b", "2026-01-01T00:00:01Z", "running") });

    expect(view.result.current).toHaveLength(2);
    expect(view.result.current?.find((r) => r.id === "b")?.status).toBe("running");
  });

  it("inserts a brand new run from a delta in the right order", async () => {
    const feed = await loadFeed();
    const view = renderHook(() => feed.useRuns());
    feed.push({ type: "snapshot", runs: [run("old", "2026-01-01T00:00:01Z")] });

    feed.push({ type: "run", run: run("new", "2026-01-01T00:00:09Z", "running") });

    expect(view.result.current?.map((r) => r.id)).toEqual(["new", "old"]);
  });
});

import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as apiModule from "./api";
import { useRuns } from "./runsFeed";

// One shared poller, and exactly one. Everything below is about the request
// count, because that is the property that broke: the feed ran two chains in
// lockstep and made two identical requests 0ms apart every two seconds.

function stubFetch() {
  return vi.spyOn(apiModule, "getIfChanged").mockResolvedValue(apiModule.Unchanged as never);
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("the shared runs feed", () => {
  // React's StrictMode does subscribe/unsubscribe/subscribe on mount. The
  // unsubscribe lands while the first request is still in flight, where
  // clearTimeout has nothing to clear — so the in-flight tick used to
  // reschedule itself anyway and the resubscribe started a second chain
  // beside it. Measured in a browser as 22 requests per 20s instead of 10.
  it("does not start a second chain when a subscriber churns mid-flight", async () => {
    const fetched = stubFetch();

    const first = renderHook(() => useRuns());
    first.unmount(); // while the initial fetch is still pending
    renderHook(() => useRuns());

    await vi.advanceTimersByTimeAsync(6100);

    // ~2s apart, so a single chain makes about 4 requests in 6s. Two chains
    // would make about twice that.
    expect(fetched.mock.calls.length).toBeLessThanOrEqual(5);
  });

  it("polls about every two seconds while something is listening", async () => {
    const fetched = stubFetch();
    renderHook(() => useRuns());

    await vi.advanceTimersByTimeAsync(6100);
    const calls = fetched.mock.calls.length;
    expect(calls).toBeGreaterThanOrEqual(3);
    expect(calls).toBeLessThanOrEqual(5);
  });

  // nanobotd is a local process people leave running all day; polling for a
  // list nobody is subscribed to is pure cost.
  it("stops when the last subscriber goes away", async () => {
    const fetched = stubFetch();

    const view = renderHook(() => useRuns());
    await vi.advanceTimersByTimeAsync(2100);
    view.unmount();
    await vi.advanceTimersByTimeAsync(100); // let any in-flight tick retire

    const afterStop = fetched.mock.calls.length;
    await vi.advanceTimersByTimeAsync(8000);
    expect(fetched.mock.calls.length).toBe(afterStop);
  });
});

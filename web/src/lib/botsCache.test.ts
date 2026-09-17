import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { invalidateBots, listBotsCached, setBotServiceConnection } from "./botsCache";

// The catalog is 30KB and three pages fetch it on mount. What is tested here
// is the pair of properties that make that safe: a repeat fetch revalidates
// instead of re-downloading, and a change to a bot is not served from the
// copy taken before it.

type Call = { path: string; ifNoneMatch: string | null; method: string };

function stubFetch(calls: Call[], status: () => number) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const h = new Headers(init?.headers);
      calls.push({
        path,
        ifNoneMatch: h.get("If-None-Match"),
        method: init?.method ?? "GET",
      });
      const code = init?.method ? 200 : status();
      if (code === 304) {
        return new Response(null, { status: 304 });
      }
      return new Response(JSON.stringify([{ id: "content-ideas" }]), {
        status: 200,
        headers: { ETag: `"v1"`, "Content-Type": "application/json" },
      });
    }),
  );
}

beforeEach(() => invalidateBots());
afterEach(() => vi.unstubAllGlobals());

describe("the bot catalog cache", () => {
  it("revalidates with the tag it was given rather than re-downloading", async () => {
    const calls: Call[] = [];
    let status = 200;
    stubFetch(calls, () => status);

    const first = await listBotsCached();
    expect(first).toHaveLength(1);
    expect(calls[0].ifNoneMatch).toBeNull(); // nothing to revalidate yet

    status = 304;
    const second = await listBotsCached();
    expect(calls[1].ifNoneMatch).toBe(`"v1"`);
    // A 304 has no body, so the answer has to come from the held copy — an
    // empty array here would blank the bot library on every navigation.
    expect(second).toEqual(first);
  });

  // The bug this exists to prevent: flip a service to live, navigate back to
  // the library, and the switch is off again because the page was served the
  // copy taken before the change. Going through the mutator is what makes
  // the invalidation impossible to forget.
  it("does not serve a bot from the copy taken before it was changed", async () => {
    const calls: Call[] = [];
    let status = 200;
    stubFetch(calls, () => status);

    await listBotsCached();
    await setBotServiceConnection("content-ideas", "gmail", true);

    status = 304; // the server would still say 304 to the old tag
    await listBotsCached();

    const gets = calls.filter((c) => c.method === "GET");
    expect(gets).toHaveLength(2);
    expect(gets[1].ifNoneMatch).toBeNull();
  });
});

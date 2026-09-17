import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The catalog is 30KB and five surfaces fetch it on mount. What is tested
// here is the pair of properties that make holding a copy safe: a repeat
// read revalidates instead of re-downloading, and a bot that just changed
// comes back changed without anyone having to invalidate anything.

type Reply = { status: number; body?: unknown; etag?: string };

function stubFetch(replies: Reply[]) {
  const sent: (string | null)[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_path: string, init?: RequestInit) => {
      sent.push(new Headers(init?.headers).get("If-None-Match"));
      const r = replies.shift();
      if (!r) throw new Error("unexpected extra fetch");
      if (r.status === 304) return new Response(null, { status: 304 });
      return new Response(JSON.stringify(r.body), {
        status: 200,
        headers: { ETag: r.etag ?? `"x"`, "Content-Type": "application/json" },
      });
    }),
  );
  return sent;
}

// Each test gets its own module instance: the tag and the held copy are
// module-level, so a shared one would make the second test depend on what
// the first left behind.
async function freshCache() {
  vi.resetModules();
  return (await import("./botsCache")).listBotsCached;
}

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => vi.unstubAllGlobals());

describe("the bot catalog cache", () => {
  it("revalidates with the tag it was given rather than re-downloading", async () => {
    const sent = stubFetch([
      { status: 200, body: [{ id: "content-ideas" }], etag: `"v1"` },
      { status: 304 },
    ]);

    const listBotsCached = await freshCache();

    const first = await listBotsCached();
    expect(first).toEqual([{ id: "content-ideas" }]);
    expect(sent[0]).toBeNull(); // nothing to revalidate yet

    const second = await listBotsCached();
    expect(sent[1]).toBe(`"v1"`);
    // A 304 has no body, so the answer has to come from the held copy — an
    // empty array here would blank the bot library on every navigation.
    expect(second).toEqual(first);
  });

  // The reason this cache needs no invalidation, asserted rather than
  // assumed. The server hashes the body it would have sent, so a bot edited
  // a millisecond ago produces a different tag and a 200 — there is no
  // window in which the held copy can be served instead. An earlier version
  // of this file had three mutator wrappers guarding against exactly the
  // staleness this shows cannot happen.
  it("returns a bot that just changed, with nobody clearing anything", async () => {
    const sent = stubFetch([
      { status: 200, body: [{ id: "inbox-triage", tuned: false }], etag: `"v1"` },
      { status: 200, body: [{ id: "inbox-triage", tuned: true }], etag: `"v2"` },
    ]);

    const listBotsCached = await freshCache();

    await listBotsCached();
    // …something POSTs an edit here; no call into this module…
    const after = await listBotsCached();

    expect(sent[1]).toBe(`"v1"`); // the tag still went up
    expect(after).toEqual([{ id: "inbox-triage", tuned: true }]);
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { listBotsCached } from "./botsCache";
import { listSwarmsCached } from "./swarmsCache";
import { revalidatingList } from "./revalidatingList";

// What makes holding a copy of a list endpoint safe, and what makes it worth
// doing. Every case here is one measured in a browser first.

type Reply = { status: number; body?: unknown; etag?: string };

function stubFetch(replies: Reply[], delayMs = 0) {
  const sent: { path: string; ifNoneMatch: string | null }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      sent.push({ path, ifNoneMatch: new Headers(init?.headers).get("If-None-Match") });
      if (delayMs) await new Promise((r) => setTimeout(r, delayMs));
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

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => vi.unstubAllGlobals());

describe("a revalidating list", () => {
  // The measured bug behind all of this: the swarm list's tag lived inside
  // the Swarms page's polling effect, so leaving the list and coming back
  // re-downloaded all 10KB to be told nothing had changed.
  it("hands a second reader the tag the first one got", async () => {
    const sent = stubFetch([{ status: 200, body: ["a"], etag: `"v1"` }, { status: 304 }]);
    const read = revalidatingList<string>("/api/things");

    const first = await read();
    expect(first).toEqual({ data: ["a"], changed: true });
    expect(sent[0].ifNoneMatch).toBeNull();

    const second = await read();
    expect(sent[1].ifNoneMatch).toBe(`"v1"`);
    // A 304 has no body, so the answer has to come from the held copy — an
    // empty array here would blank the page on every navigation. And
    // `changed: false` is what lets a poller skip the state update, so an
    // idle list costs one empty round trip and no render.
    expect(second).toEqual({ data: ["a"], changed: false });
  });

  // Why there is nothing to invalidate. The server hashes the body it would
  // have sent, so something edited a millisecond ago produces a different
  // tag and a 200 — there is no window in which the held copy is served
  // instead. The bot cache first shipped with three mutator wrappers
  // guarding against exactly the staleness this shows cannot happen.
  it("returns a row that just changed, with nobody clearing anything", async () => {
    const sent = stubFetch([
      { status: 200, body: ["before"], etag: `"v1"` },
      { status: 200, body: ["after"], etag: `"v2"` },
    ]);
    const read = revalidatingList<string>("/api/things");

    await read();
    // …something POSTs an edit here; no call into the cache…
    const after = await read();

    expect(sent[1].ifNoneMatch).toBe(`"v1"`); // the tag still went up
    expect(after).toEqual({ data: ["after"], changed: true });
  });

  // The shell's count-up and the Swarms page's first poll both mount at
  // t=0. Neither had a tag yet, so both sent none and both got the full
  // 10KB — two of the four /api/swarms requests in a five-page browse.
  it("makes one request for readers that arrive together", async () => {
    const sent = stubFetch([{ status: 200, body: ["a"], etag: `"v1"` }], 20);
    const read = revalidatingList<string>("/api/things");

    const [one, two] = await Promise.all([read(), read()]);

    expect(sent).toHaveLength(1);
    expect(one).toEqual(two);
    // And the next reader starts a fresh one rather than being stuck on the
    // answer from before.
    stubFetch([{ status: 304 }]);
    expect((await read()).changed).toBe(false);
  });

  it("never reports an empty list it was not told about", async () => {
    stubFetch([{ status: 304 }]);
    const read = revalidatingList<string>("/api/things");

    expect(await read()).toEqual({ data: [], changed: false });
  });
});

// Each cache is one line of wrapping around the above, so the only thing
// left that can be wrong is which endpoint it asks for — and both callers
// swallow the error, so a wrong path is an empty page rather than a noise.
describe("the two lists this app holds", () => {
  it("ask for the endpoints they claim to", async () => {
    const sent = stubFetch([
      { status: 200, body: [], etag: `"v1"` },
      { status: 200, body: [], etag: `"v1"` },
    ]);
    await listBotsCached();
    await listSwarmsCached();
    expect(sent.map((s) => s.path)).toEqual(["/api/bots", "/api/swarms"]);
  });
});

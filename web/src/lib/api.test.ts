import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";

describe("api.blobUrl", () => {
  it("turns an nbf:// reference into a blob endpoint path", () => {
    expect(api.blobUrl("nbf://sha256/abc123")).toBe("/api/blobs/abc123");
  });

  it("passes mime through as a query param when given", () => {
    expect(api.blobUrl("nbf://sha256/abc123", "application/pdf")).toBe(
      "/api/blobs/abc123?mime=application%2Fpdf",
    );
  });
});

// The backend's every error response is {"error": "..."} (writeError, the
// one place any handler builds one) — a caller that just wrapped the raw
// body showed a user the literal JSON: submitting to the composer with no
// model configured read `Error: 400 Bad Request:
// {"error":"the composer needs a model — ..."}` on screen.
describe("api error messages", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("shows the backend's own message, not the raw JSON envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "the composer needs a model" }), {
          status: 400,
          statusText: "Bad Request",
        }),
      ),
    );
    // Exact equality, not a substring match — toThrow("x") passes as long
    // as "x" appears *anywhere* in the message, which is also true of the
    // raw envelope this test exists to catch: {"error":"the composer
    // needs a model"} still contains the substring "the composer needs a
    // model". A first version of this test asserted only that substring
    // and stayed green with the bug still in place.
    await expect(api.status()).rejects.toThrow(
      expect.objectContaining({ message: "the composer needs a model" }),
    );
  });

  it("falls back to the raw body when the response isn't the usual {error} shape", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response("upstream proxy error", { status: 502, statusText: "Bad Gateway" }),
        ),
    );
    await expect(api.status()).rejects.toThrow("502 Bad Gateway: upstream proxy error");
  });
});

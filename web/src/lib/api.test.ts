import { describe, expect, it } from "vitest";
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

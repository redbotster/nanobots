import { describe, expect, it, vi, afterEach } from "vitest";
import { relativeTime } from "./relativeTime";

describe("relativeTime", () => {
  const now = new Date("2026-03-02T12:00:00Z");

  afterEach(() => {
    vi.useRealTimers();
  });

  function at(iso: string) {
    vi.useFakeTimers();
    vi.setSystemTime(now);
    return relativeTime(iso);
  }

  it("says 'just now' for anything under 10 seconds old", () => {
    expect(at("2026-03-02T11:59:55Z")).toBe("just now");
  });

  it("shows seconds under a minute", () => {
    expect(at("2026-03-02T11:59:30Z")).toBe("30s ago");
  });

  it("shows minutes under an hour", () => {
    expect(at("2026-03-02T11:45:00Z")).toBe("15m ago");
  });

  it("shows hours under a day", () => {
    expect(at("2026-03-02T09:00:00Z")).toBe("3h ago");
  });

  it("shows days under a week", () => {
    expect(at("2026-02-28T12:00:00Z")).toBe("2d ago");
  });

  it("falls back to a locale date for a week or older", () => {
    const got = at("2026-02-20T12:00:00Z");
    expect(got).not.toContain("ago");
  });

  it("returns the raw input for an unparsable date rather than throwing", () => {
    expect(relativeTime("not-a-date")).toBe("not-a-date");
  });
});

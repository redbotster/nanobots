import { describe, expect, it, vi, afterEach } from "vitest";
import { relativeTime, timeOf, untilTime } from "./relativeTime";

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

describe("untilTime", () => {
  const inSeconds = (s: number) => new Date(Date.now() + s * 1000).toISOString();

  it("phrases near-future times", () => {
    expect(untilTime(inSeconds(45))).toBe("in 45s");
    expect(untilTime(inSeconds(60 * 5))).toBe("in 5m");
    expect(untilTime(inSeconds(60 * 60 * 3))).toBe("in 3h");
    expect(untilTime(inSeconds(60 * 60 * 24 * 2))).toBe("in 2d");
  });

  // A schedule that's due, or that fired a moment ago and hasn't been
  // recomputed yet, must never render as "in -3s".
  it("never counts backwards", () => {
    expect(untilTime(inSeconds(-5))).toBe("any moment now");
    expect(untilTime(inSeconds(-3600))).toBe("any moment now");
    expect(untilTime(inSeconds(10))).toBe("any moment now");
  });

  it("falls back to a date beyond a week", () => {
    const far = inSeconds(60 * 60 * 24 * 30);
    expect(untilTime(far)).toBe(new Date(far).toLocaleDateString());
  });

  it("passes through something that isn't a date", () => {
    expect(untilTime("not a date")).toBe("not a date");
  });
});

// Extracted from RunLog.tsx and LabPage.tsx, which each defined this
// verbatim — one place per fact, same reason relativeTime/untilTime live
// here rather than beside whichever page happened to need one first.
describe("timeOf", () => {
  it("renders a 24-hour clock time with seconds", () => {
    expect(timeOf("2026-03-02T14:05:09Z")).toBe(
      new Date("2026-03-02T14:05:09Z").toLocaleTimeString([], { hour12: false }),
    );
  });
});

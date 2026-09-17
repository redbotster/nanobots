import { describe, expect, it, beforeEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useEntered } from "./entered";

describe("useEntered", () => {
  beforeEach(() => localStorage.clear());

  it("starts on the landing page", () => {
    const { result } = renderHook(() => useEntered());
    expect(result.current[0]).toBe(false);
  });

  // The reason this hook exists: clicking past the landing page used to be a
  // React state flip, so every reload put the wall back up.
  it("remembers that you already went past it", () => {
    const first = renderHook(() => useEntered());
    act(() => first.result.current[1](true));
    expect(first.result.current[0]).toBe(true);

    const reload = renderHook(() => useEntered());
    expect(reload.result.current[0]).toBe(true);
  });

  // The logo in the dashboard header goes back, so this is not a one-way
  // door — and going back has to stick, or the next reload bounces you in.
  it("forgets again when you go back", () => {
    const { result } = renderHook(() => useEntered());
    act(() => result.current[1](true));
    act(() => result.current[1](false));
    expect(result.current[0]).toBe(false);
    expect(renderHook(() => useEntered()).result.current[0]).toBe(false);
  });
});

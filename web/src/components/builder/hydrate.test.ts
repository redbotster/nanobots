import { describe, expect, it } from "vitest";
import { placeBots, toCanvasSnaps } from "./hydrate";

const pos = (i: number) => ({ x: i * 10, y: 0 });

describe("loading a saved swarm into the builder", () => {
  // The builder saves the whole swarm back, so a field it doesn't load is a
  // field it deletes. That is how "Save changes" once removed a cron
  // trigger, and both of these were one line from the same fate.
  it("keeps on_error", () => {
    const [plain, tolerant] = placeBots(
      [
        { id: "a", use: "alpha@0.1.0" },
        { id: "b", use: "beta@0.1.0", on_error: "continue" },
      ],
      pos,
    );
    expect(plain.onError).toBeUndefined();
    expect(tolerant.onError).toBe("continue");
  });

  it("keeps join", () => {
    const [plain, joined] = toCanvasSnaps([
      { from: "a.x", to: "b.y" },
      { from: "b.z", to: "c.w", join: "lines" },
    ]);
    expect(plain.join).toBeUndefined();
    expect(joined.join).toBe("lines");
  });

  it("splits the versioned use into a bot id", () => {
    expect(placeBots([{ id: "recap", use: "recap-emails-to-pdf@0.3.0" }], pos)[0].botId).toBe(
      "recap-emails-to-pdf",
    );
  });
});

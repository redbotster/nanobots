import { describe, expect, it } from "vitest";
import { placeBots, toCanvasSnaps, toDraftBot } from "./hydrate";
import type { PlacedBot } from "./BuilderCanvas";
import type { BotSummary } from "../../lib/types";

const pos = (i: number) => ({ x: i * 10, y: 0 });

function placedBot(overrides: Partial<PlacedBot>): PlacedBot {
  return { instanceId: "b1", botId: "notify", x: 0, y: 0, ...overrides };
}

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

  // retry, execution, retry_backoff, when, fallback, loop and swarm all
  // reached the wire (internal/api/builder.go's builderBotRef) before this
  // mapping caught up to them — the same "loads, then silently vanishes on
  // save" gap on_error and join were once one line from, just unnoticed
  // because no catalog swarm used any of the five newer ones yet.
  it("keeps retry, execution, retry_backoff, when, fallback, loop and swarm", () => {
    const [placed] = placeBots(
      [
        {
          id: "poller",
          use: "watch-the-competition@0.1.0",
          retry: 3,
          execution: "container",
          retry_backoff: "5s",
          when: "{{inputs.amount}} > 500",
          fallback: "fixture-fetch@0.1.0",
          loop: { max: 20, until: "{{outputs.done}}" },
        },
      ],
      pos,
    );
    expect(placed.retry).toBe(3);
    expect(placed.execution).toBe("container");
    expect(placed.retryBackoff).toBe("5s");
    expect(placed.when).toBe("{{inputs.amount}} > 500");
    expect(placed.fallback).toBe("fixture-fetch@0.1.0");
    expect(placed.loop).toEqual({ max: 20, until: "{{outputs.done}}" });
  });

  it("keeps a nested swarm reference, with no catalog bot id to derive", () => {
    const [placed] = placeBots([{ id: "refund", use: "", swarm: "refund-flow.yaml" }], pos);
    expect(placed.swarm).toBe("refund-flow.yaml");
  });
});

describe("saving the builder's state back to the wire shape", () => {
  const botDefs: Record<string, BotSummary> = {
    notify: { id: "notify", version: "0.1.0" } as BotSummary,
  };

  it("reconstructs use from the bot id and its catalog version", () => {
    const draft = toDraftBot(placedBot({ botId: "notify" }), botDefs);
    expect(draft.use).toBe("notify@0.1.0");
    expect(draft.swarm).toBeUndefined();
  });

  it("carries retry, execution, retry_backoff, when, fallback and loop through unchanged", () => {
    const draft = toDraftBot(
      placedBot({
        retry: 3,
        execution: "container",
        retryBackoff: "5s",
        when: "{{inputs.amount}} > 500",
        fallback: "fixture-fetch@0.1.0",
        loop: { max: 20, until: "{{outputs.done}}" },
      }),
      botDefs,
    );
    expect(draft.retry).toBe(3);
    expect(draft.execution).toBe("container");
    expect(draft.retry_backoff).toBe("5s");
    expect(draft.when).toBe("{{inputs.amount}} > 500");
    expect(draft.fallback).toBe("fixture-fetch@0.1.0");
    expect(draft.loop).toEqual({ max: 20, until: "{{outputs.done}}" });
  });

  // The bug this exists to catch: a nested-swarm bot has no catalog botId,
  // so botId is "" and botDefs[""] is never found. Reconstructing `use:`
  // the normal way for one would silently write `use: "@0.0.0"` over its
  // real identity — turning a swarm reference into a broken bot reference,
  // which is worse than the field simply going missing.
  it("does not fabricate a use for a nested-swarm bot", () => {
    const draft = toDraftBot(placedBot({ botId: "", swarm: "refund-flow.yaml" }), botDefs);
    expect(draft.use).toBe("");
    expect(draft.swarm).toBe("refund-flow.yaml");
  });
});

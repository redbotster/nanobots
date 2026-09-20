import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SwarmView } from "./SwarmView";
import * as useRunModule from "../lib/useRun";
import { api } from "../lib/api";
import * as botsCacheModule from "../lib/botsCache";
import type { SwarmSummary } from "../lib/types";

function swarm(over: Partial<SwarmSummary>): SwarmSummary {
  return {
    path: "examples/swarms/daily-email-recap.yaml",
    name: "daily-email-recap",
    description: "d",
    services_live: 0,
    services_total: 2,
    ...over,
  } as SwarmSummary;
}

function stubApi() {
  vi.spyOn(api, "plan").mockResolvedValue({
    swarm: "daily-email-recap",
    order: [],
    bots: [],
    snaps: [],
  } as never);
  vi.spyOn(api, "listBots").mockResolvedValue([] as never);
}

afterEach(() => vi.restoreAllMocks());

describe("opening a swarm", () => {
  // The dead end this fixes: the page only knew about runs started from it,
  // so it always said "Nothing's run yet" — including for a swarm whose run
  // was at that moment sitting on an approval. Following "waiting for your
  // approval" from the swarm list landed you on a page claiming nothing had
  // ever run, with no way to answer from there.
  it("watches the swarm's most recent run instead of starting blank", async () => {
    stubApi();
    const useRun = vi.spyOn(useRunModule, "useRun").mockReturnValue({
      run: null,
      isTerminal: false,
    } as never);

    render(
      <SwarmView
        swarm={swarm({ last_run_id: "run-abc", last_run_status: "awaiting_approval" })}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(useRun).toHaveBeenCalledWith("run-abc");
    });
  });

  it("still starts blank for a swarm that genuinely has never run", async () => {
    stubApi();
    const useRun = vi.spyOn(useRunModule, "useRun").mockReturnValue({
      run: null,
      isTerminal: false,
    } as never);

    render(<SwarmView swarm={swarm({})} onBack={() => {}} />);

    await waitFor(() => {
      expect(useRun).toHaveBeenCalledWith(null);
    });
    expect(screen.getByText(/nothing's run yet/i)).toBeTruthy();
  });
});

// morning-brief's own shape: `meetings` has no snap at all, and `triage`
// feeds `notifier` directly — skipping over `brief`, which sits between
// them in run order. Both are real catalog cases (docs/parallelism.md: 5 of
// 18 swarms branch), and both used to draw a connector with a fabricated
// "output" -> "input" label — a wire between two bots that share no data,
// indistinguishable on screen from a real one.
describe("the connector between two adjacent bots", () => {
  function stubBranchingPlan() {
    vi.spyOn(api, "plan").mockResolvedValue({
      swarm: "morning-brief",
      order: ["meetings", "triage", "brief", "notifier"],
      bots: [
        { instance_id: "meetings", bot_id: "meeting-prep", name: "meeting-prep", version: "0.1.0" },
        { instance_id: "triage", bot_id: "inbox-triage", name: "inbox-triage", version: "0.1.0" },
        { instance_id: "brief", bot_id: "render-pdf", name: "render-pdf", version: "0.1.0" },
        { instance_id: "notifier", bot_id: "notify", name: "notify", version: "0.1.0" },
      ],
      snaps: [
        { From: "triage.triaged_count.brief", To: "brief.content" },
        { From: "triage.triaged_count.headline", To: "notifier.message" },
      ],
      ok: true,
    } as never);
    // SwarmView reads the catalog through listBotsCached (../lib/botsCache),
    // not api.listBots directly — stubApi()'s api.listBots mock above is
    // never actually consulted by this page, only by tests that never need
    // real bot data to render.
    vi.spyOn(botsCacheModule, "listBotsCached").mockResolvedValue([
      { id: "meeting-prep", name: "meeting-prep", version: "0.1.0" },
      { id: "inbox-triage", name: "inbox-triage", version: "0.1.0" },
      { id: "render-pdf", name: "render-pdf", version: "0.1.0" },
      { id: "notify", name: "notify", version: "0.1.0" },
    ] as never);
  }

  it("draws no connector for two adjacent bots with no real snap between them", async () => {
    stubBranchingPlan();
    vi.spyOn(useRunModule, "useRun").mockReturnValue({ run: null, isTerminal: false } as never);

    render(<SwarmView swarm={swarm({ name: "morning-brief" })} onBack={() => {}} />);

    await waitFor(() => expect(screen.queryAllByText("meeting-prep").length).toBeGreaterThan(0));
    // Three adjacent pairs total: meetings->triage (no snap), triage->brief
    // (real), brief->notifier (no snap — the real one is triage->notifier,
    // which skips over brief). Exactly one real trail should render; the
    // other two pairs must not fabricate one.
    expect(screen.getAllByLabelText(/^Snap:/)).toHaveLength(1);
  });

  it("still draws a real connector when one backs the adjacent pair", async () => {
    stubBranchingPlan();
    vi.spyOn(useRunModule, "useRun").mockReturnValue({ run: null, isTerminal: false } as never);

    render(<SwarmView swarm={swarm({ name: "morning-brief" })} onBack={() => {}} />);

    // triage -> brief is both adjacent in run order AND a real snap.
    await waitFor(() =>
      expect(screen.queryByLabelText(/^Snap: triaged_count\.brief to content/)).toBeTruthy(),
    );
  });
});

// Before wiringByInstance existed, every input dot was plain-muted and
// every output dot was plain-bright regardless of whether *that specific*
// port was actually fed by a real snap — a fixed styling convention, not a
// reflection of the swarm. This is the full pipeline: plan.snaps and
// plan.unfed, through SwarmView's own wiring map, into the dot each real
// BotBrick renders.
describe("a bot's port dots reflect the real plan, end to end", () => {
  function stubTwoBotPlan() {
    vi.spyOn(api, "plan").mockResolvedValue({
      swarm: "two-bot-chain",
      order: ["chaser", "sender"],
      bots: [
        {
          instance_id: "chaser",
          bot_id: "invoice-chaser",
          name: "invoice-chaser",
          version: "0.1.0",
        },
        {
          instance_id: "sender",
          bot_id: "email-send-approved",
          name: "email-send-approved",
          version: "0.1.0",
        },
      ],
      snaps: [{ From: "chaser.drafted", To: "sender.draft_id" }],
      unfed: [{ bot: "sender", port: "summary", reason: "nothing supplies it" }],
      ok: true,
    } as never);
    vi.spyOn(botsCacheModule, "listBotsCached").mockResolvedValue([
      {
        id: "invoice-chaser",
        name: "invoice-chaser",
        version: "0.1.0",
        description: "",
        tags: [],
        harness: "bare",
        services: [],
        inputs: [],
        outputs: [
          { name: "overdue", type: "list<json>" },
          { name: "drafted", type: "list<json>" },
        ],
        guardrails: {},
      },
      {
        id: "email-send-approved",
        name: "email-send-approved",
        version: "0.1.0",
        description: "",
        tags: [],
        harness: "bare",
        services: [],
        inputs: [
          { name: "draft_id", type: "string", required: true },
          { name: "summary", type: "string", required: true },
        ],
        outputs: [],
        guardrails: {},
      },
    ] as never);
  }

  it("marks only the specific wired and unfed ports, not every port on that side", async () => {
    stubTwoBotPlan();
    vi.spyOn(useRunModule, "useRun").mockReturnValue({ run: null, isTerminal: false } as never);

    const { container } = render(
      <SwarmView swarm={swarm({ name: "two-bot-chain" })} onBack={() => {}} />,
    );
    await waitFor(() => expect(screen.queryAllByText("invoice-chaser").length).toBeGreaterThan(0));

    // One output wrap per bot, in render order: chaser's is first.
    const outputWraps = [...container.querySelectorAll('[class*="-right-1.5"]')];
    const chaserOutputs = [...outputWraps[0].querySelectorAll("span")];
    expect(chaserOutputs[0].className).toContain("border-muted"); // overdue: not read by anything
    expect(chaserOutputs[1].className).toContain("border-tron"); // drafted: feeds sender.draft_id

    const inputWraps = [...container.querySelectorAll('[class*="-left-1.5"]')];
    const senderInputs = [...inputWraps[1].querySelectorAll("span")];
    expect(senderInputs[0].className).toContain("border-tron"); // draft_id: wired
    expect(senderInputs[1].className).toContain("border-danger"); // summary: required, unfed
  });
});

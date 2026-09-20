import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LastRunLine, ScheduleLine, SwarmsPage } from "./SwarmsPage";
import { swarmMatches } from "../lib/swarmFilter";
import { api } from "../lib/api";
import * as runsFeed from "../lib/runsFeed";
import * as swarmsCacheModule from "../lib/swarmsCache";
import type { SwarmSummary } from "../lib/types";

function swarm(over: Partial<SwarmSummary>): SwarmSummary {
  return {
    path: "examples/swarms/content-engine.yaml",
    name: "content-engine",
    description: "d",
    services_live: 0,
    services_total: 2,
    ...over,
  } as SwarmSummary;
}

describe("the swarm card's run line", () => {
  // The failure this app actually has: 54 runs on the development machine
  // died at their container timeout with an approval nobody answered, 27
  // hours of container time. The whole time, the card for that swarm said
  // "last ran 2h ago" in muted grey — past tense about a run that had not
  // finished, and nothing to suggest a person was being waited on.
  it("says a swarm is waiting on you, not when it last ran", () => {
    render(
      <LastRunLine
        swarm={swarm({
          last_run_status: "awaiting_approval",
          last_run_at: "2026-09-15T10:00:00Z",
        })}
      />,
    );
    expect(screen.getByText(/waiting for your approval/i)).toBeTruthy();
    expect(screen.queryByText(/last ran/i)).toBeNull();
  });

  // v3 Phase 4: a passkey-locked 1Claw vault pauses a run too, but it is
  // not the same wait as an approval — there is no button in this app that
  // answers it, so the wording must not send someone looking for one.
  it("says a swarm is waiting on 1Claw's vault, distinctly from an approval", () => {
    render(
      <LastRunLine
        swarm={swarm({
          last_run_status: "awaiting_unlock",
          last_run_at: "2026-09-15T10:00:00Z",
        })}
      />,
    );
    expect(screen.getByText(/1claw's vault/i)).toBeTruthy();
    expect(screen.queryByText(/waiting for your approval/i)).toBeNull();
    expect(screen.queryByText(/last ran/i)).toBeNull();
  });

  // Same bug, smaller stakes: a run in progress is not a run that happened.
  it("says a running swarm is running, not when it last ran", () => {
    render(
      <LastRunLine
        swarm={swarm({ last_run_status: "running", last_run_at: "2026-09-15T10:00:00Z" })}
      />,
    );
    expect(screen.getByText(/running now/i)).toBeTruthy();
    expect(screen.queryByText(/last ran/i)).toBeNull();
  });

  it("still says when a finished run last ran", () => {
    render(
      <LastRunLine
        swarm={swarm({
          last_run_status: "succeeded",
          last_run_at: new Date(Date.now() - 3600_000).toISOString(),
          last_run_trigger: "schedule",
        })}
      />,
    );
    expect(screen.getByText(/last ran/i)).toBeTruthy();
    expect(screen.getByText(/scheduled/i)).toBeTruthy();
  });

  // The gap the live UI audit found: a run that finished with a tolerated
  // failure (get-paid's reminders went out, notify didn't) reported
  // last_run_status "succeeded" and rendered in plain green, identical to
  // a run where nothing was skipped.
  it("warns when the last run succeeded but skipped a step", () => {
    const { container } = render(
      <LastRunLine
        swarm={swarm({
          last_run_status: "succeeded",
          last_run_at: new Date(Date.now() - 3600_000).toISOString(),
          last_run_tolerated: 1,
        })}
      />,
    );
    expect(screen.getByText(/last ran/i)).toBeTruthy();
    expect(screen.getByText(/1 step didn't run/i)).toBeTruthy();
    expect(container.querySelector("span")?.className).toContain("bg-warn");
  });

  it("stays plain green when the last run succeeded with nothing tolerated", () => {
    const { container } = render(
      <LastRunLine
        swarm={swarm({
          last_run_status: "succeeded",
          last_run_at: new Date(Date.now() - 3600_000).toISOString(),
        })}
      />,
    );
    expect(screen.queryByText(/didn't run/i)).toBeNull();
    expect(container.querySelector("span")?.className).toContain("bg-ok");
  });

  it("says nothing has run yet when nothing has", () => {
    render(<LastRunLine swarm={swarm({})} />);
    expect(screen.getByText(/never run yet/i)).toBeTruthy();
  });
});

describe("loading the swarm list on mount", () => {
  afterEach(() => vi.restoreAllMocks());

  // The shared cache in swarmsCache.ts is a module-level singleton: it can
  // already hold a fresh ETag from an earlier mount of this page (Swarms ->
  // Lab -> Swarms), so this remounted instance's very first poll can come
  // back `changed: false` even though it has never painted a list. That
  // used to leave the page reading "Loading swarms..." forever, since
  // nothing else populates `swarms` on mount.
  it("shows the list even when the first poll reports nothing changed", async () => {
    // SwarmsPage renders GettingStarted, which reads the run feed and the
    // connections list — through the shared feed here, not this test's
    // concern, so stub both the same way GettingStarted's own tests do.
    vi.spyOn(runsFeed, "useRuns").mockReturnValue([]);
    vi.spyOn(api, "listConnections").mockResolvedValue([]);
    vi.spyOn(swarmsCacheModule, "listSwarmsCached").mockResolvedValue({
      swarms: [swarm({ name: "morning-brief" })],
      changed: false,
    });

    render(<SwarmsPage uiMode="advanced" status={null} onOpenSettings={() => {}} />);

    await waitFor(() => expect(screen.getByText("morning-brief")).toBeTruthy());
    expect(screen.queryByText(/loading swarms/i)).toBeNull();
  });
});

describe("filtering the swarm list", () => {
  const list = [
    swarm({
      name: "github-digest-to-slack",
      description: "Summarise a repo's newest open issues and post the digest to Slack.",
    }),
    swarm({
      name: "get-paid",
      description: "Find every overdue invoice and send each reminder once approved.",
    }),
    swarm({
      name: "morning-brief",
      description: "Triage the inbox and prep today's meetings into one brief.",
    }),
  ];
  const matching = (q: string) => list.filter((s) => swarmMatches(s, q)).map((s) => s.name);

  it("matches on the name", () => {
    expect(matching("get-paid")).toEqual(["get-paid"]);
  });

  it("matches on the description, because that is how people remember one", () => {
    // "the one that posts to Slack" is a description search; the word
    // appears in github-digest-to-slack's name too, which is fine — it is
    // the same swarm either way.
    expect(matching("invoice")).toEqual(["get-paid"]);
    expect(matching("meetings")).toEqual(["morning-brief"]);
  });

  it("does not care about the order the words are remembered in", () => {
    expect(matching("slack digest")).toEqual(["github-digest-to-slack"]);
    expect(matching("digest slack")).toEqual(["github-digest-to-slack"]);
  });

  it("ignores case and stray whitespace", () => {
    expect(matching("  GitHub   DIGEST ")).toEqual(["github-digest-to-slack"]);
  });

  it("shows everything when nothing has been typed", () => {
    expect(matching("")).toHaveLength(3);
    expect(matching("   ")).toHaveLength(3);
  });

  it("returns nothing rather than everything when nothing matches", () => {
    expect(matching("nonexistent")).toEqual([]);
  });
});

describe("a swarm that pauses for you", () => {
  // Seven of the sixteen catalog swarms fire on a timer and then wait for a
  // human. That combination is the largest source of failed runs on the
  // machine this was built on — 54 of them — and the card said nothing: it
  // showed a time and left you to find out from a run that died overnight.
  it("says so on the schedule line", () => {
    render(
      <ScheduleLine
        swarm={swarm({
          schedule: "Weekdays at 7:00 AM",
          schedule_expr: "0 7 * * 1-5",
          trigger_type: "cron",
          needs_approval: true,
        })}
      />,
    );
    expect(screen.getByText(/pauses for you/i)).toBeTruthy();
  });

  it("stays quiet for a swarm that runs straight through", () => {
    render(
      <ScheduleLine
        swarm={swarm({
          schedule: "Weekdays at 7:00 AM",
          schedule_expr: "0 7 * * 1-5",
          trigger_type: "cron",
        })}
      />,
    );
    expect(screen.queryByText(/pauses for you/i)).toBeNull();
  });
});

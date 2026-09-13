import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImportSwarm } from "./ImportSwarm";
import { api } from "../lib/api";

afterEach(() => vi.restoreAllMocks());

function paste(text: string) {
  fireEvent.click(screen.getByRole("button", { name: /Add a shared swarm/ }));
  fireEvent.change(screen.getByRole("textbox"), { target: { value: text } });
}

describe("ImportSwarm", () => {
  it("shows what the swarm will do before offering to add it", async () => {
    const spy = vi.spyOn(api, "importSwarm").mockResolvedValue({
      name: "get-paid",
      acts: ["gmail", "slack"],
      connects: ["google"],
      imported: false,
    });
    render(<ImportSwarm onImported={() => {}} />);
    paste("format: 1\nname: get-paid\n");
    fireEvent.click(screen.getByRole("button", { name: "Read it" }));

    // The first call must not write. That is the whole reason this is two
    // steps: a swarm is executable and `acts` is what you read first.
    await waitFor(() => expect(spy).toHaveBeenCalledWith(expect.any(String), false));
    expect(screen.getByText(/can write to: gmail, slack/)).toBeTruthy();

    // Only now is there a button that writes.
    fireEvent.click(screen.getByRole("button", { name: "Add it" }));
    await waitFor(() => expect(spy).toHaveBeenCalledWith(expect.any(String), true));
  });

  it("does not offer to add a bundle whose bots are missing", async () => {
    vi.spyOn(api, "importSwarm").mockResolvedValue({
      name: "needs-more",
      missing: ["no-such-bot@9.9.9"],
      imported: false,
    });
    render(<ImportSwarm onImported={() => {}} />);
    paste("format: 1\nname: needs-more\n");
    fireEvent.click(screen.getByRole("button", { name: "Read it" }));

    await waitFor(() => expect(screen.getByText(/no-such-bot@9.9.9/)).toBeTruthy());
    // No "Add it" — the backend would refuse, and offering the button
    // anyway makes the refusal look like a bug rather than the answer.
    expect(screen.queryByRole("button", { name: "Add it" })).toBeNull();
  });

  it("reports a bundle it cannot read instead of failing silently", async () => {
    vi.spyOn(api, "importSwarm").mockRejectedValue(
      new Error("this is not a swarm bundle — if it is a plain swarm YAML, copy it into examples/swarms/ instead"),
    );
    render(<ImportSwarm onImported={() => {}} />);
    paste("kind: Nanoswarm\n");
    fireEvent.click(screen.getByRole("button", { name: "Read it" }));

    await waitFor(() => expect(screen.getByText(/not a swarm bundle/)).toBeTruthy());
  });
});

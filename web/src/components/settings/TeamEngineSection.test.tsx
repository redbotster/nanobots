import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TeamEngineSection } from "./TeamEngineSection";
import { api } from "../../lib/api";
import type { LabEnginesResponse } from "../../lib/types";

afterEach(() => vi.restoreAllMocks());

function engines(over: Partial<LabEnginesResponse>): LabEnginesResponse {
  return {
    default_engine: "",
    claude_configured: false,
    gemini_configured: false,
    roles: [],
    ...over,
  };
}

describe("TeamEngineSection", () => {
  it("asks for a key before offering any engine choice", async () => {
    vi.spyOn(api, "labEngines").mockResolvedValue(engines({}));
    render(<TeamEngineSection />);

    await waitFor(() => expect(screen.getByText("Team engine")).toBeTruthy());
    expect(screen.getByText(/before Lab can delegate anything/)).toBeTruthy();
    expect(screen.queryByText("Default")).toBeNull();
  });

  it("offers a default-engine picker once a key is configured", async () => {
    vi.spyOn(api, "labEngines").mockResolvedValue(
      engines({ default_engine: "gemini", gemini_configured: true }),
    );
    render(<TeamEngineSection />);

    await waitFor(() => expect(screen.getByText("Default")).toBeTruthy());
    // Claude isn't configured, so it must not be offered as a choice.
    expect(screen.queryByRole("option", { name: "Claude Code" })).toBeNull();
    expect(screen.getByRole("option", { name: "Gemini CLI" })).toBeTruthy();
  });

  it("says plainly that no role has been delegated to yet", async () => {
    vi.spyOn(api, "labEngines").mockResolvedValue(
      engines({ default_engine: "claude", claude_configured: true }),
    );
    render(<TeamEngineSection />);
    await waitFor(() => expect(screen.getByText(/No role has been delegated to yet/)).toBeTruthy());
  });

  it("lists every role that has one, with its effective engine picked", async () => {
    vi.spyOn(api, "labEngines").mockResolvedValue(
      engines({
        default_engine: "claude",
        claude_configured: true,
        gemini_configured: true,
        roles: [
          { role: "designer", engine: "gemini", overridden: true },
          { role: "backend-engineer", engine: "claude", overridden: false },
        ],
      }),
    );
    render(<TeamEngineSection />);

    // "designer" also names the example in this section's own intro
    // paragraph, so the role row itself needs a more specific query.
    await waitFor(() => expect(screen.getByText("designer", { selector: ".flex-1" })).toBeTruthy());
    const roleRow = screen.getByText("designer", { selector: ".flex-1" }).parentElement!;
    // designer's select shows its override; backend-engineer's shows "(default — ...)".
    expect(within(roleRow).getByRole("combobox")).toHaveValue("gemini");
  });

  it("changing the default calls the API and refreshes", async () => {
    const labEngines = vi
      .spyOn(api, "labEngines")
      .mockResolvedValueOnce(
        engines({ default_engine: "claude", claude_configured: true, gemini_configured: true }),
      )
      .mockResolvedValueOnce(
        engines({ default_engine: "gemini", claude_configured: true, gemini_configured: true }),
      );
    const setDefault = vi
      .spyOn(api, "setDefaultLabEngine")
      .mockResolvedValue({ default_engine: "gemini" });
    render(<TeamEngineSection />);

    await waitFor(() => expect(screen.getByText("Default")).toBeTruthy());
    const select = screen.getByDisplayValue("Claude Code") as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "gemini" } });

    await waitFor(() => expect(setDefault).toHaveBeenCalledWith("gemini"));
    await waitFor(() => expect(labEngines).toHaveBeenCalledTimes(2));
  });

  it("pasting a key saves it and shows the restart notice", async () => {
    vi.spyOn(api, "labEngines").mockResolvedValue(engines({}));
    const setKey = vi.spyOn(api, "setLabEngineKey").mockResolvedValue({
      saved: "gemini/api_key",
      notice: "restart nanobotd to make this key available to Team",
    });
    render(<TeamEngineSection />);

    await waitFor(() => expect(screen.getByText("Gemini CLI")).toBeTruthy());
    fireEvent.click(screen.getAllByRole("button", { name: "Add key" })[1]);
    fireEvent.change(screen.getByPlaceholderText("Paste key…"), {
      target: { value: "AIzaSy-test" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(setKey).toHaveBeenCalledWith("gemini", "AIzaSy-test"));
    // "restart nanobotd" also appears in the no-engine-configured warning
    // above, so match the row's own notice specifically.
    await waitFor(() =>
      expect(screen.getByText(/restart nanobotd to make this key available to Team/)).toBeTruthy(),
    );
  });
});

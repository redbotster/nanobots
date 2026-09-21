import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GuardrailsEditor } from "./GuardrailsEditor";
import { api } from "../lib/api";
import type { Guardrails } from "../lib/types";

afterEach(() => vi.restoreAllMocks());

const current: Guardrails = {
  pii: "redact",
  injection_threshold: 0.7,
  max_runtime_secs: 120,
  daily_budget_usd: 5,
  network_egress: ["api.stripe.com", "googleapis.com"],
  writes_allowed: ["gmail"],
  approval_required_for: [],
};

describe("GuardrailsEditor", () => {
  it("collapsed, summarizes the policy without opening the full form", () => {
    render(
      <GuardrailsEditor
        botId="x"
        current={current}
        shipped={current}
        tuned={false}
        onChanged={vi.fn()}
      />,
    );
    expect(screen.getByText(/redact/)).toBeTruthy();
    expect(screen.getByText(/120s max/)).toBeTruthy();
    expect(screen.getByText(/\$5\/day/)).toBeTruthy();
    expect(screen.queryByText("What this bot is allowed to do")).toBeNull();
  });

  it("opens to a form pre-filled from the current values", () => {
    render(
      <GuardrailsEditor
        botId="x"
        current={current}
        shipped={current}
        tuned={false}
        onChanged={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText(/redact/));
    expect(screen.getByText("What this bot is allowed to do")).toBeTruthy();
    expect(screen.getByDisplayValue("api.stripe.com, googleapis.com")).toBeTruthy();
    expect(screen.getByDisplayValue("gmail")).toBeTruthy();
  });

  it("saves the edited values in the wire shape the API expects", async () => {
    const save = vi.spyOn(api, "setBotGuardrails").mockResolvedValue({} as never);
    const onChanged = vi.fn();
    render(
      <GuardrailsEditor
        botId="inbox-triage"
        current={current}
        shipped={current}
        tuned={false}
        onChanged={onChanged}
      />,
    );
    fireEvent.click(screen.getByText(/redact/));
    fireEvent.change(screen.getByDisplayValue("redact"), { target: { value: "block" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        "inbox-triage",
        expect.objectContaining({
          pii: "block",
          network_egress: ["api.stripe.com", "googleapis.com"],
        }),
      ),
    );
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  // A stray comma or trailing space in a pasted list must not become a
  // guardrail entry that silently matches nothing real.
  it("trims and drops blank entries from a comma-separated list", async () => {
    const save = vi.spyOn(api, "setBotGuardrails").mockResolvedValue({} as never);
    render(
      <GuardrailsEditor
        botId="x"
        current={current}
        shipped={current}
        tuned={false}
        onChanged={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText(/redact/));
    fireEvent.change(screen.getByDisplayValue("api.stripe.com, googleapis.com"), {
      target: { value: " a.com ,, b.com ," },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        "x",
        expect.objectContaining({ network_egress: ["a.com", "b.com"] }),
      ),
    );
  });

  it("does not offer to put anything back until it's actually been tuned", () => {
    render(
      <GuardrailsEditor
        botId="x"
        current={current}
        shipped={current}
        tuned={false}
        onChanged={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText(/redact/));
    expect(screen.queryByText("Put back what it shipped with")).toBeNull();
  });

  it("put back saves the shipped values, not the current (edited) ones", async () => {
    const shipped: Guardrails = { pii: "allow", max_runtime_secs: 60 };
    const save = vi.spyOn(api, "setBotGuardrails").mockResolvedValue({} as never);
    render(
      <GuardrailsEditor botId="x" current={current} shipped={shipped} tuned onChanged={vi.fn()} />,
    );
    fireEvent.click(screen.getByText(/redact/));
    fireEvent.click(screen.getByRole("button", { name: "Put back what it shipped with" }));

    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        "x",
        expect.objectContaining({ pii: "allow", max_runtime_secs: 60, network_egress: [] }),
      ),
    );
  });

  it("disables Save until something has actually changed", () => {
    render(
      <GuardrailsEditor
        botId="x"
        current={current}
        shipped={current}
        tuned={false}
        onChanged={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText(/redact/));
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    fireEvent.change(screen.getByDisplayValue("redact"), { target: { value: "block" } });
    expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled();
  });
});

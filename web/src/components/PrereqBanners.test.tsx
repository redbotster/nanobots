import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PrereqBanners } from "./PrereqBanners";
import type { StatusResponse } from "../lib/types";

function status(over: Partial<StatusResponse>): StatusResponse {
  return {
    oneclaw_configured: true,
    llm_backend: "1claw shroud",
    llm_guardrails: true,
    memory_backend: "local",
    memory_recall: false,
    docker_available: true,
    vault_locked: false,
    ...over,
  } as StatusResponse;
}

// The banner sits at the top of every page, so what it says when Docker is
// missing is the first thing a new person reads about whether this works.
//
// It used to say "nothing can run until you start Docker Desktop". That
// stopped being true when 34 of the 39 bots moved in-process, and a banner
// announcing the product is dead while almost all of it works is a worse
// bug than the dependency it is reporting — it is also the exact claim the
// README contradicts two lines into the install section.
describe("the Docker banner", () => {
  it("does not claim nothing runs without Docker", () => {
    render(
      <PrereqBanners
        status={status({ docker_available: false, docker_reason: "Docker isn't installed" })}
      />,
    );
    const text = document.body.innerText || document.body.textContent || "";
    expect(text).toMatch(/most bots run without it/i);
    expect(text).not.toMatch(/nothing can run/i);
    expect(text).not.toMatch(/to run anything for real/i);
  });

  it("says what Docker actually buys you", () => {
    render(
      <PrereqBanners
        status={status({ docker_available: false, docker_reason: "Docker isn't running" })}
      />,
    );
    const text = document.body.innerText || document.body.textContent || "";
    expect(text).toMatch(/render a PDF or a chart/i);
  });

  it("says nothing at all when Docker is there", () => {
    render(<PrereqBanners status={status({ docker_available: true })} />);
    expect(screen.queryByText(/Docker/i)).toBeNull();
  });

  // The other two banners are unrelated to Docker and must still fire.
  it("still reports a missing model and a locked vault", () => {
    render(<PrereqBanners status={status({ llm_backend: "none", vault_locked: true })} />);
    expect(screen.getByText(/No model configured/i)).toBeTruthy();
    expect(screen.getByText(/vault is locked/i)).toBeTruthy();
  });
});

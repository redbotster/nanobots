import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusDot } from "./StatusDot";

describe("StatusDot", () => {
  it("renders an ok-toned dot", () => {
    const { container } = render(<StatusDot tone="ok" />);
    expect(container.querySelector("span")?.className).toContain("bg-ok");
  });

  it("defaults to muted", () => {
    const { container } = render(<StatusDot />);
    expect(container.querySelector("span")?.className).toContain("bg-muted");
  });
});

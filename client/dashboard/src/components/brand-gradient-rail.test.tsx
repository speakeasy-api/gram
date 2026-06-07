import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { BrandGradientRail } from "./brand-gradient-rail";

afterEach(cleanup);

describe("BrandGradientRail", () => {
  it("renders an aria-hidden bar", () => {
    const { container } = render(<BrandGradientRail />);
    const el = container.firstChild as HTMLElement;
    expect(el.getAttribute("aria-hidden")).toBe("true");
    // Note: happy-dom drops `var()` inside `linear-gradient` from the parsed
    // CSSOM, so the exact gradient value is verified visually, not here. We
    // only assert the element renders with the expected accessibility contract.
  });

  it("merges a passed className", () => {
    const { container } = render(<BrandGradientRail className="left-0" />);
    expect((container.firstChild as HTMLElement).className).toContain("left-0");
  });
});

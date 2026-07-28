import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BrandGradientLine } from "./brand-gradient-line";

describe("BrandGradientLine", () => {
  it("renders the shared mount-animation class without changing its decorative contract", () => {
    const { container } = render(<BrandGradientLine className="test-class" />);
    const line = container.firstElementChild;

    expect(line?.getAttribute("aria-hidden")).toBe("true");
    expect(line?.classList.contains("brand-gradient-line")).toBe(true);
    expect(line?.classList.contains("test-class")).toBe(true);
    expect((line as HTMLElement).style.background).toBe(
      "linear-gradient(90deg, var(--gradient-brand-primary-colors))",
    );
  });
});

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ProductTier } from "@/hooks/useProductTier";
import { EnterpriseGate } from "./enterprise-gate";

const tier = vi.hoisted(() => ({ current: "base" as ProductTier }));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => tier.current,
}));

afterEach(cleanup);

const renderGate = (productTier: ProductTier) => {
  tier.current = productTier;
  render(
    <EnterpriseGate>
      <div>gated content</div>
    </EnterpriseGate>,
  );
};

describe("EnterpriseGate", () => {
  it.each<ProductTier>(["enterprise", "payg"])(
    "renders the children for the %s tier",
    (productTier) => {
      renderGate(productTier);
      expect(screen.getByText("gated content")).toBeTruthy();
    },
  );

  it.each<ProductTier>(["base", "base_PAID", "__deprecated__pro"])(
    "renders the upsell for the %s tier",
    (productTier) => {
      renderGate(productTier);
      expect(screen.queryByText("gated content")).toBeNull();
      expect(screen.getByText("Enterprise Feature")).toBeTruthy();
    },
  );
});

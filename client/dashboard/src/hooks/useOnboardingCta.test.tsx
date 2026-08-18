import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ProductTier } from "@/hooks/useProductTier";
import { useOnboardingCta } from "./useOnboardingCta";

const state = vi.hoisted(() => ({
  tier: "base" as ProductTier,
  orgSlug: "an-org" as string | undefined,
  isAdmin: true,
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => state.tier,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: state.orgSlug }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => state.isAdmin && scope === "org:admin",
  }),
}));

const renderEligible = (overrides: Partial<typeof state>): boolean => {
  Object.assign(
    state,
    { tier: "base", orgSlug: "an-org", isAdmin: true },
    overrides,
  );
  const { result } = renderHook(() => useOnboardingCta());

  return result.current.eligible;
};

describe("useOnboardingCta", () => {
  it.each<ProductTier>(["enterprise", "payg"])(
    "makes an org admin on the %s tier eligible",
    (tier) => {
      expect(renderEligible({ tier })).toBe(true);
    },
  );

  it.each<ProductTier>(["base", "base_PAID", "__deprecated__pro"])(
    "leaves an org admin on the %s tier ineligible",
    (tier) => {
      expect(renderEligible({ tier })).toBe(false);
    },
  );

  it("requires org admin and an org slug", () => {
    expect(renderEligible({ tier: "payg", isAdmin: false })).toBe(false);
    expect(renderEligible({ tier: "payg", orgSlug: undefined })).toBe(false);
  });
});

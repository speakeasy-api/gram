import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Locks in CTA eligibility: enterprise + org:admin only. No code change — this
// guards the enterprise-trial onboarding CTA gate.

const state = vi.hoisted(() => ({
  orgSlug: "acme" as string | undefined,
  productTier: "enterprise" as string,
  scopes: ["org:admin"] as string[],
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: state.orgSlug }),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => state.productTier,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: (s: string) => state.scopes.includes(s) }),
}));

vi.mock("@/hooks/useDismissedCtaStore", () => ({
  createDismissedCtaStore: () => ({
    useDismissed: () => false,
    write: vi.fn(),
  }),
}));

vi.mock("@/lib/view-transition", () => ({
  withViewTransition: (fn: () => void) => fn(),
}));

import { useOnboardingCta } from "./useOnboardingCta";

beforeEach(() => {
  state.orgSlug = "acme";
  state.productTier = "enterprise";
  state.scopes = ["org:admin"];
});

afterEach(() => vi.clearAllMocks());

describe("useOnboardingCta", () => {
  it("is eligible for an enterprise org admin", () => {
    const { result } = renderHook(() => useOnboardingCta());
    expect(result.current.eligible).toBe(true);
  });

  it("is not eligible for a non-enterprise org", () => {
    state.productTier = "free";
    const { result } = renderHook(() => useOnboardingCta());
    expect(result.current.eligible).toBe(false);
  });

  it("is not eligible for a non-admin member", () => {
    state.scopes = [];
    const { result } = renderHook(() => useOnboardingCta());
    expect(result.current.eligible).toBe(false);
  });
});

import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useCanSetUpOrg, useCanViewOrgSetup } from "./useCanSetUpOrg";
const state = vi.hoisted(() => ({
  admin: false,
  read: true,
  loading: false,
  error: null as Error | null,
  tier: "free",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/hooks/useProductTier", () => ({ useProductTier: () => state.tier }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.loading,
    error: state.error,
    hasScope: (scope: string) =>
      scope === "org:admin" ? state.admin : state.read,
  }),
}));
afterEach(() => {
  cleanup();
  Object.assign(state, {
    admin: false,
    read: true,
    loading: false,
    error: null,
    tier: "free",
  });
});
it("makes onboarding discoverable to unassigned readers regardless of tier", () => {
  expect(renderHook(useCanViewOrgSetup).result.current).toBe(true);
  expect(renderHook(useCanSetUpOrg).result.current).toBe(false);
});
it.each(["payg", "enterprise"])(
  "retains the %s admin promotional CTA",
  (tier) => {
    state.tier = tier;
    state.admin = true;
    expect(renderHook(useCanSetUpOrg).result.current).toBe(true);
  },
);
it("does not expose navigation with loading, failed, or denied grants", () => {
  state.loading = true;
  const hook = renderHook(useCanViewOrgSetup);
  expect(hook.result.current).toBe(false);
  state.loading = false;
  state.error = new Error("unavailable");
  hook.rerender();
  expect(hook.result.current).toBe(false);
  state.error = null;
  state.read = false;
  hook.rerender();
  expect(hook.result.current).toBe(false);
});

import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useProviderSetupRollout } from "./useProviderSetupRollout";

const mocks = vi.hoisted(() => ({
  organizationId: "org-one",
  features: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.organizationId }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: mocks.features,
}));
afterEach(cleanup);
it.each([
  [{ data: { providerSetupEnabled: true } }, "enabled"],
  [{ data: { providerSetupEnabled: false } }, "disabled"],
  [{ data: {} }, "disabled"],
  [{ data: undefined }, "disabled"],
  [{ data: { providerSetupEnabled: true }, isPending: true }, "loading"],
  [{ data: undefined, isPending: true }, "loading"],
  [{ data: { providerSetupEnabled: true }, isError: true }, "disabled"],
] as const)("uses the organization's rollout state %j", (query, expected) => {
  mocks.features.mockReturnValue(query);
  const { result } = renderHook(() => useProviderSetupRollout());
  expect(result.current).toBe(expected);
});
it("requests the current organization's features when organizations change", () => {
  mocks.features.mockReturnValue({ data: { providerSetupEnabled: true } });
  const { rerender } = renderHook(() => useProviderSetupRollout());
  expect(mocks.features).toHaveBeenLastCalledWith(
    { organizationId: "org-one" },
    undefined,
    { throwOnError: false },
  );
  mocks.organizationId = "org-two";
  rerender();
  expect(mocks.features).toHaveBeenLastCalledWith(
    { organizationId: "org-two" },
    undefined,
    { throwOnError: false },
  );
});

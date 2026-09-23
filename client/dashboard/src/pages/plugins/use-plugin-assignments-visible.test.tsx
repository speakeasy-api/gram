import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  admin: false,
  query: vi.fn(() => ({ data: { deviceAgent: true } })),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.admin, isLoading: false }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: state.query,
}));
import { usePluginAssignmentsVisible } from "./use-plugin-assignments-visible";
it("does not fetch or expose cached organization features to plugin-only writers", () => {
  const { result } = renderHook(usePluginAssignmentsVisible);
  expect(state.query).toHaveBeenLastCalledWith(
    { organizationId: "org-a" },
    undefined,
    expect.objectContaining({ enabled: false }),
  );
  expect(result.current).toBe(false);
});

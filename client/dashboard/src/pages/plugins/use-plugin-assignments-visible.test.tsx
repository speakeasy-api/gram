import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  grants: [] as string[],
  loading: false,
  feature: true,
  telemetry: false,
  hasScope: vi.fn(),
  query: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => state.telemetry }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: state.hasScope, isLoading: state.loading }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: state.query,
}));
import { usePluginAssignmentsVisible } from "./use-plugin-assignments-visible";

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.grants = ["project-a:project:write", "org-a:org:read"];
  state.loading = false;
  state.feature = true;
  state.telemetry = false;
  state.hasScope.mockImplementation((scope: string, resource: string) =>
    state.grants.includes(`${resource}:${scope}`),
  );
  // Return cached data even when disabled to exercise the visibility gate.
  state.query.mockImplementation(() => ({
    data: { deviceAgent: state.feature },
  }));
});

function expectAccess(enabled: boolean) {
  expect(state.hasScope).toHaveBeenCalledWith("org:admin", "org-a");
  expect(state.query).toHaveBeenLastCalledWith(
    { organizationId: "org-a" },
    undefined,
    expect.objectContaining({ enabled }),
  );
}

it("does not fetch or expose cached organization features to plugin-only writers", () => {
  const { result } = renderHook(usePluginAssignmentsVisible);
  expectAccess(false);
  expect(result.current).toBe(false);
});

it("fetches and exposes enabled organization features for an admin", () => {
  state.grants = ["org-a:org:admin"];
  const { result } = renderHook(usePluginAssignmentsVisible);
  expectAccess(true);
  expect(result.current).toBe(true);
});

it("does not accept an admin grant for another organization", () => {
  state.grants = ["org-b:org:admin"];
  const { result } = renderHook(usePluginAssignmentsVisible);
  expectAccess(false);
  expect(result.current).toBe(false);
});

it("masks cached features after an admin is downgraded", () => {
  state.grants = ["org-a:org:admin"];
  const { result, rerender } = renderHook(usePluginAssignmentsVisible);
  expectAccess(true);
  expect(result.current).toBe(true);
  state.grants = ["org-a:org:read", "project-a:project:write"];
  state.hasScope.mockClear();
  rerender();
  expectAccess(false);
  expect(result.current).toBe(false);
});

it("does not enable assignments for an admin without either feature flag", () => {
  state.grants = ["org-a:org:admin"];
  state.feature = false;
  const { result } = renderHook(usePluginAssignmentsVisible);
  expectAccess(true);
  expect(result.current).toBe(false);
});

it.each([false, true])(
  "keeps the telemetry override behind admin access (admin: %s)",
  (admin) => {
    state.grants = admin ? ["org-a:org:admin"] : ["org-a:org:read"];
    state.feature = false;
    state.telemetry = true;
    const { result } = renderHook(usePluginAssignmentsVisible);
    expectAccess(admin);
    expect(result.current).toBe(admin);
  },
);

it("does not fetch or expose features while grants load", () => {
  state.loading = true;
  state.grants = ["org-a:org:admin"];
  state.telemetry = true;
  const { result } = renderHook(usePluginAssignmentsVisible);
  expect(state.query).toHaveBeenLastCalledWith(
    { organizationId: "org-a" },
    undefined,
    expect.objectContaining({ enabled: false }),
  );
  expect(result.current).toBe(false);
});

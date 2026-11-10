import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  grants: [] as string[],
  loading: false,
  hasScope: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: state.hasScope, isLoading: state.loading }),
}));
import { usePluginAssignmentsVisible } from "./use-plugin-assignments-visible";

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.grants = ["project-a:project:write", "org-a:org:read"];
  state.loading = false;
  state.hasScope.mockImplementation((scope: string, resource: string) =>
    state.grants.includes(`${resource}:${scope}`),
  );
});

it("does not expose assignments to plugin-only writers", () => {
  const { result } = renderHook(usePluginAssignmentsVisible);
  expect(result.current).toBe(false);
});

it("exposes assignments for an organization admin", () => {
  state.grants = ["org-a:org:admin"];
  const { result } = renderHook(usePluginAssignmentsVisible);
  expect(result.current).toBe(true);
});

it("does not accept an admin grant for another organization", () => {
  state.grants = ["org-b:org:admin"];
  const { result } = renderHook(usePluginAssignmentsVisible);
  expect(result.current).toBe(false);
});

it("hides assignments after an admin is downgraded", () => {
  state.grants = ["org-a:org:admin"];
  const { result, rerender } = renderHook(usePluginAssignmentsVisible);
  expect(result.current).toBe(true);
  state.grants = ["org-a:org:read", "project-a:project:write"];
  state.hasScope.mockClear();
  rerender();
  expect(result.current).toBe(false);
});

it("does not expose assignments while grants load", () => {
  state.loading = true;
  state.grants = ["org-a:org:admin"];
  const { result } = renderHook(usePluginAssignmentsVisible);
  expect(result.current).toBe(false);
});

import { cleanup, renderHook } from "@testing-library/react";
import type { RoleGrant } from "@gram/client/models/components/rolegrant.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useModeSwitcherEnabled } from "./mode-switch-context";

const state = vi.hoisted(() => ({
  grants: undefined as RoleGrant[] | undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "test-session" }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@gram/client/react-query/grants.js", () => ({
  useGrants: () => ({
    data: state.grants === undefined ? undefined : { grants: state.grants },
    isLoading: state.grants === undefined,
    error: undefined,
  }),
}));

afterEach(cleanup);

describe("useModeSwitcherEnabled", () => {
  beforeEach(() => {
    state.grants = [];
  });

  it("shows the switcher for organization admins", () => {
    state.grants = [
      {
        scope: "org:admin",
        selectors: [{ resourceKind: "org", resourceId: "test-org" }],
      },
    ];
    const { result } = renderHook(() => useModeSwitcherEnabled());
    expect(result.current).toBe(true);
  });

  it.each(["project:read", "project:write"] as const)(
    "keeps the switcher hidden for a project-only %s grant",
    (scope) => {
      state.grants = [
        {
          scope,
          selectors: [{ resourceKind: "project", resourceId: "test-project" }],
        },
      ];
      const { result } = renderHook(() => useModeSwitcherEnabled());
      expect(result.current).toBe(false);
    },
  );

  it("keeps the switcher hidden while grants are loading", () => {
    state.grants = undefined;
    const { result } = renderHook(() => useModeSwitcherEnabled());
    expect(result.current).toBe(false);
  });

  it("hides the switcher when admin access is removed", () => {
    state.grants = [{ scope: "org:admin" }];
    const { result, rerender } = renderHook(() => useModeSwitcherEnabled());
    expect(result.current).toBe(true);
    state.grants = [{ scope: "project:write" }];
    rerender();
    expect(result.current).toBe(false);
  });
});

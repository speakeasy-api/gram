import { ListUserGrantsResult$inboundSchema } from "@gram/client/models/components/listusergrantsresult.js";
import { Scope$inboundSchema } from "@gram/client/models/components/rolegrant.js";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { hasAnyUnblockedScopeInGrants, useRBAC } from "./useRBAC";

const mocks = vi.hoisted(() => ({ useGrants: vi.fn() }));
vi.mock("@gram/client/react-query/grants.js", () => ({
  useGrants: mocks.useGrants,
}));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => false,
  useSession: () => ({ session: "test-session" }),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function loadGrants(grants: unknown[]) {
  const data = ListUserGrantsResult$inboundSchema.parse({ grants });
  mocks.useGrants.mockReturnValue({ data, isLoading: false, error: null });
  return renderHook(() => useRBAC()).result;
}

describe("forward-compatible grants", () => {
  it("keeps known grants usable alongside unknown scopes, sub-scopes, and selectors", () => {
    const result = loadGrants([
      {
        scope: "future:read",
        selectors: [
          {
            resource_kind: "future",
            resource_id: "*",
            disposition: "future_disposition",
          },
        ],
      },
      {
        scope: "project:write",
        sub_scopes: ["project:read", "project:future"],
        selectors: [{ resource_kind: "project", resource_id: "project_a" }],
      },
    ]);

    expect(result.current.error).toBeNull();
    expect(result.current.grants[0]?.scope).toBe("future:read");
    expect(result.current.hasScope("project:read", "project_a")).toBe(true);
    expect(result.current.hasScope("project:read", "project_b")).toBe(false);
    expect(result.current.hasScope("org:admin")).toBe(false);
  });

  it("does not authorize known sub-scopes through an unknown parent, including direct grants", () => {
    const result = loadGrants([
      {
        scope: "future:admin",
        sub_scopes: ["org:admin", "project:read"],
        direct_selectors: [{ resource_kind: "*", resource_id: "project_a" }],
      },
    ]);

    expect(result.current.hasScope("org:admin")).toBe(false);
    expect(result.current.hasAnyScope(["org:admin", "project:read"])).toBe(
      false,
    );
    expect(
      result.current.hasAnyScopeInProject(["project:read"], "project_a"),
    ).toBe(false);
  });

  it("rejects unknown requested scopes even when a grant or sub-scope matches", () => {
    const scope = Scope$inboundSchema.parse("project:future");
    const result = loadGrants([
      { scope: "project:future" },
      { scope: "project:write", sub_scopes: ["project:future"] },
    ]);

    expect(result.current.hasScope(scope)).toBe(false);
    expect(result.current.hasAllScopes([scope])).toBe(false);
    expect(
      hasAnyUnblockedScopeInGrants(result.current.grants, [
        { scope, resourceId: "project_a" },
      ]),
    ).toBe(false);
  });

  it("does not let unknown direct grants override a known exclusion", () => {
    const selector = { resource_kind: "mcp", resource_id: "server_a" };
    const result = loadGrants([
      { scope: "mcp:connect", selectors: [selector] },
      { scope: "mcp:blocked_connect", selectors: [selector] },
      {
        scope: "mcp:future",
        sub_scopes: ["mcp:connect"],
        direct_selectors: [selector],
      },
    ]);

    expect(result.current.hasScope("mcp:connect", "server_a")).toBe(false);
  });

  it.each([null, 42, {}, []])(
    "still rejects invalid scope types: %j",
    (scope) => {
      expect(
        ListUserGrantsResult$inboundSchema.safeParse({ grants: [{ scope }] })
          .success,
      ).toBe(false);
    },
  );
});

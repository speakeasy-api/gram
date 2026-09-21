import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { usePluginWriteAccess } from "./usePluginWriteAccess";

const state = vi.hoisted(() => ({
  isLoading: false,
  grants: [] as Array<{
    scope: string;
    effect?: string;
    selectors?: Array<Record<string, string>>;
  }>,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
  useProject: () => ({ id: "project-a" }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => state,
}));
afterEach(cleanup);
beforeEach(() => {
  state.isLoading = false;
  state.grants = [];
});
const allowed = () => renderHook(usePluginWriteAccess).result.current;

describe("plugin write capability", () => {
  it.each(["org:admin", "plugin:write"])(
    "allows %s without skill write",
    (scope) => {
      state.grants = [{ scope }];
      expect(allowed()).toBe(true);
    },
  );
  it.each(["skill:write", "skill:read", "mcp:write", "org:read"])(
    "does not elevate %s",
    (scope) => {
      state.grants = [{ scope }];
      expect(allowed()).toBe(false);
    },
  );
  it("matches project selectors, not plugin or skill identifiers", () => {
    state.grants = [
      {
        scope: "plugin:write",
        selectors: [
          {
            resourceKind: "project",
            resourceId: "project-a",
            projectId: "project-a",
          },
        ],
      },
    ];
    expect(allowed()).toBe(true);
    state.grants[0]!.selectors![0]!.projectId = "project-b";
    expect(allowed()).toBe(false);
  });
  it.each(["plugin:blocked_write", "org:blocked_admin", "org:blocked_read"])(
    "honors %s even with both allows",
    (scope) => {
      state.grants = [
        { scope: "plugin:write" },
        { scope: "org:admin" },
        { scope },
      ];
      expect(allowed()).toBe(false);
    },
  );
  it.each(["plugin:write", "org:admin"])(
    "honors a legacy deny of %s",
    (scope) => {
      state.grants = [
        { scope: "plugin:write" },
        { scope: "org:admin" },
        { scope, effect: "deny" },
      ];
      expect(allowed()).toBe(false);
    },
  );
  it("ignores plugin exclusions in another project", () => {
    state.grants = [
      { scope: "org:admin" },
      {
        scope: "plugin:blocked_write",
        selectors: [
          {
            resourceKind: "project",
            resourceId: "project-b",
            projectId: "project-b",
          },
        ],
      },
    ];
    expect(allowed()).toBe(true);
  });
  it("does not turn skill editing exclusions into reference exclusions", () => {
    state.grants = [
      { scope: "plugin:write" },
      { scope: "skill:blocked_write" },
    ];
    expect(allowed()).toBe(true);
  });
  it("fails closed while loading grants", () => {
    state.grants = [{ scope: "plugin:write" }];
    state.isLoading = true;
    expect(allowed()).toBe(false);
  });
});

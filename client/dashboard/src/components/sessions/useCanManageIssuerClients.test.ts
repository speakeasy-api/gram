import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useCanManageIssuerClients } from "./useCanManageIssuerClients";

const state = vi.hoisted(() => ({
  issuer: undefined as { projectId?: string } | undefined,
  scopes: [] as string[],
}));

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: () => ({ data: state.issuer }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_1" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resource: string) =>
      state.scopes.includes(`${scope}@${resource}`),
  }),
}));

describe("useCanManageIssuerClients", () => {
  afterEach(() => {
    state.issuer = undefined;
    state.scopes = [];
  });

  it("reads false until the issuer has loaded", () => {
    state.scopes = ["project:write@project_1", "org:admin@org_1"];
    const { result } = renderHook(() => useCanManageIssuerClients("issuer_1"));
    expect(result.current).toBe(false);
  });

  it("needs project:write on the project that owns the issuer", () => {
    state.issuer = { projectId: "project_1" };
    state.scopes = ["org:admin@org_1"];
    expect(
      renderHook(() => useCanManageIssuerClients("issuer_1")).result.current,
    ).toBe(false);

    state.scopes = ["project:write@project_1"];
    expect(
      renderHook(() => useCanManageIssuerClients("issuer_1")).result.current,
    ).toBe(true);
  });

  it("needs org:admin for an organization-owned issuer", () => {
    state.issuer = { projectId: "" };
    state.scopes = ["project:write@project_1"];
    expect(
      renderHook(() => useCanManageIssuerClients("issuer_1")).result.current,
    ).toBe(false);

    state.scopes = ["org:admin@org_1"];
    expect(
      renderHook(() => useCanManageIssuerClients("issuer_1")).result.current,
    ).toBe(true);
  });
});

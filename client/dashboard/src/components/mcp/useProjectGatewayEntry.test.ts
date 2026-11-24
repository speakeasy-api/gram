import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useProjectGatewayEntry } from "./useProjectGatewayEntry";

const state = vi.hoisted(() => ({
  projectRead: true,
  orgAdmin: false,
  permissionsLoading: false,
  isLoading: false,
  data: undefined as
    | {
        available: boolean;
        url?: string;
        unavailableReason?: string;
      }
    | undefined,
  lastOptions: undefined as
    | { enabled?: boolean; throwOnError?: boolean }
    | undefined,
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "sales" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "sales",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId: string) =>
      (scope === "project:read" &&
        resourceId === "project-1" &&
        state.projectRead) ||
      (scope === "org:admin" && state.orgAdmin),
    isLoading: state.permissionsLoading,
  }),
}));
vi.mock("@gram/client/react-query/projectGateway.js", () => ({
  useProjectGateway: (
    _req: unknown,
    _opts: unknown,
    queryOptions: { enabled?: boolean; throwOnError?: boolean },
  ) => {
    state.lastOptions = queryOptions;
    return {
      data: state.data,
      isLoading: state.isLoading,
    };
  },
}));

beforeEach(() => {
  state.projectRead = true;
  state.orgAdmin = false;
  state.permissionsLoading = false;
  state.isLoading = false;
  state.data = undefined;
  state.lastOptions = undefined;
});

describe("useProjectGatewayEntry", () => {
  it("shows the entry as available with its URL when served", () => {
    state.data = {
      available: true,
      url: "https://api.example.test/mcp/gateway/acme/projects/sales",
    };

    const { result } = renderHook(() => useProjectGatewayEntry());

    expect(result.current).toEqual({
      show: true,
      status: "available",
      url: "https://api.example.test/mcp/gateway/acme/projects/sales",
    });
  });

  it("keeps the entry visible as off for non-admin project members", () => {
    state.orgAdmin = false;
    state.data = {
      available: false,
      unavailableReason: "turned_off",
    };

    const { result } = renderHook(() => useProjectGatewayEntry());

    expect(result.current).toEqual({
      show: true,
      status: "off",
      url: undefined,
    });
  });

  it.each([
    "no_organization_issuer",
    "multiple_organization_issuers",
    "custom_domain_only",
    "organization_disabled",
  ])("keeps the entry visible as unavailable when reason is %s", (reason) => {
    state.data = {
      available: false,
      unavailableReason: reason,
    };

    const { result } = renderHook(() => useProjectGatewayEntry());

    expect(result.current).toEqual({
      show: true,
      status: "unavailable",
      url: undefined,
    });
  });

  it("keeps the entry visible as unavailable when the gateway read fails", () => {
    state.data = undefined;
    state.isLoading = false;

    const { result } = renderHook(() => useProjectGatewayEntry());

    expect(result.current).toEqual({
      show: true,
      status: "unavailable",
      url: undefined,
    });
  });

  it("reports loading while the gateway read is pending", () => {
    state.isLoading = true;

    const { result } = renderHook(() => useProjectGatewayEntry());

    expect(result.current).toEqual({
      show: true,
      status: "loading",
      url: undefined,
    });
  });

  it("hides the entry and disables the query without project:read", () => {
    state.projectRead = false;

    const { result } = renderHook(() => useProjectGatewayEntry());

    expect(result.current.show).toBe(false);
    expect(state.lastOptions?.enabled).toBe(false);
  });
});

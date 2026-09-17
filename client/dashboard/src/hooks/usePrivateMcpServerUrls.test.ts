import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  privateMcpEndpointUrls,
  usePrivateMcpServerUrls,
} from "./usePrivateMcpServerUrls";

import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import { renderHook } from "@testing-library/react";

const hookState = vi.hoisted(() => ({
  rolloutEnabled: true,
  canManageIngress: true,
  isSuccess: true,
  isFetching: false,
  isPending: false,
  isError: false,
}));

vi.mock("@/hooks/useNetworkIngressRollout", () => ({
  useNetworkIngressRollout: () => ({
    rolloutEnabled: hookState.rolloutEnabled,
    canManageIngress: hookState.canManageIngress,
  }),
}));

vi.mock("@gram/client/react-query/networkIngress.js", () => ({
  useNetworkIngress: () => ({
    data: {
      ingress: {
        dnsName: "private.example.ts.net",
        endpointNamespaceKind: "platform",
        enabled: true,
        status: "online",
      },
    },
    isSuccess: hookState.isSuccess,
    isFetching: hookState.isFetching,
    isPending: hookState.isPending,
    isError: hookState.isError,
  }),
}));

const endpoints: McpEndpoint[] = [
  {
    id: "platform-endpoint",
    projectId: "project-1",
    mcpServerId: "server-1",
    slug: "platform-server",
    isDomainRoot: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  },
  {
    id: "custom-endpoint",
    projectId: "project-1",
    mcpServerId: "server-1",
    customDomainId: "domain-1",
    slug: "custom-server",
    isDomainRoot: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  },
];

const onlineIngress = {
  dnsName: "private.example.ts.net",
  endpointNamespaceKind: "platform" as const,
  customDomainId: undefined,
  enabled: true,
  status: "online",
};

beforeEach(() => {
  hookState.rolloutEnabled = true;
  hookState.canManageIngress = true;
  hookState.isSuccess = true;
  hookState.isFetching = false;
  hookState.isPending = false;
  hookState.isError = false;
});

describe("privateMcpEndpointUrls", () => {
  it("returns only endpoints in the ingress namespace", () => {
    expect(privateMcpEndpointUrls(onlineIngress, endpoints)).toEqual([
      "https://private.example.ts.net/mcp/platform-server",
    ]);
  });

  it.each([
    { enabled: false, status: "deleting" },
    { enabled: true, status: "pending" },
    { enabled: true, status: "error" },
  ])("does not advertise an unavailable ingress", (state) => {
    expect(
      privateMcpEndpointUrls({ ...onlineIngress, ...state }, endpoints),
    ).toEqual([]);
  });
});

describe("usePrivateMcpServerUrls", () => {
  const privateServer = { networkAccessMode: "private_only" as const };

  it("returns online private URLs for an authorized viewer", () => {
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls(privateServer, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([
      "https://private.example.ts.net/mcp/platform-server",
    ]);
    expect(result.current.canReadPrivateUrls).toBe(true);
  });

  it("discards cached ingress data after admin access is revoked", () => {
    hookState.canManageIngress = false;
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls(privateServer, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([]);
    expect(result.current.privateInstallPageUrls).toEqual([]);
    expect(result.current.canReadPrivateUrls).toBe(false);
  });

  it("suppresses cached private URLs while ingress state refetches", () => {
    hookState.isFetching = true;
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls(privateServer, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([]);
    expect(result.current.isLoading).toBe(true);
  });

  it("discards cached private URLs when the query fails", () => {
    hookState.isSuccess = false;
    hookState.isError = true;
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls(privateServer, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([]);
    expect(result.current.isError).toBe(true);
  });

  it("discards cached private URLs after switching to public-only", () => {
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls({ networkAccessMode: "public_only" }, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });
});

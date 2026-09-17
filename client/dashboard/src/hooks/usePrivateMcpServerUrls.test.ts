import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  mcpServerInstallPageLinks,
  privateMcpEndpointUrls,
  privateMcpInstallPageUrls,
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

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://api.example.com",
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

describe("privateMcpInstallPageUrls", () => {
  it("hosts private install pages on Gram instead of the tailnet", () => {
    expect(privateMcpInstallPageUrls(onlineIngress, endpoints)).toEqual([
      "https://api.example.com/mcp/platform-server/install?network=private",
    ]);
  });

  it.each([
    { enabled: false, status: "disabled" },
    { enabled: true, status: "pending" },
    { enabled: true, status: "error" },
  ])("does not link an unavailable private install route", (state) => {
    expect(
      privateMcpInstallPageUrls({ ...onlineIngress, ...state }, endpoints),
    ).toEqual([]);
  });
});

describe("mcpServerInstallPageLinks", () => {
  const publicInstallPageUrl = "https://public.example.com/mcp/server/install";
  const privateInstallPageUrls = [
    "https://app.example.com/mcp/server/install?network=private",
    "https://app.example.com/mcp/additional/install?network=private",
  ];

  it("labels public and private links explicitly in dual mode", () => {
    expect(
      mcpServerInstallPageLinks(
        "dual",
        publicInstallPageUrl,
        privateInstallPageUrls,
      ),
    ).toEqual([
      { url: publicInstallPageUrl, label: "Public install page" },
      { url: privateInstallPageUrls[0], label: "Private install page" },
      {
        url: privateInstallPageUrls[1],
        label: "Private install page 2",
      },
    ]);
  });

  it("does not mislabel private links when dual mode has no public link", () => {
    expect(
      mcpServerInstallPageLinks("dual", undefined, privateInstallPageUrls),
    ).toEqual([
      { url: privateInstallPageUrls[0], label: "Private install page" },
      {
        url: privateInstallPageUrls[1],
        label: "Private install page 2",
      },
    ]);
  });

  it("omits the public link in private-only mode", () => {
    expect(
      mcpServerInstallPageLinks(
        "private_only",
        publicInstallPageUrl,
        privateInstallPageUrls,
      ),
    ).toEqual([
      { url: privateInstallPageUrls[0], label: "Private install page" },
      {
        url: privateInstallPageUrls[1],
        label: "Private install page 2",
      },
    ]);
  });
});

describe("usePrivateMcpServerUrls", () => {
  const privateServer = { networkAccessMode: "private_only" as const };

  it.each(["dual", "private_only"] as const)(
    "returns online private URLs for an authorized %s server",
    (networkAccessMode) => {
      const { result } = renderHook(() =>
        usePrivateMcpServerUrls({ networkAccessMode }, endpoints),
      );

      expect(result.current.privateMcpUrls).toEqual([
        "https://private.example.ts.net/mcp/platform-server",
      ]);
      expect(result.current.privateInstallPageUrls).toEqual([
        "https://api.example.com/mcp/platform-server/install?network=private",
      ]);
      expect(result.current.canReadPrivateUrls).toBe(true);
    },
  );

  it("discards cached ingress data after admin access is revoked", () => {
    hookState.canManageIngress = false;
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls(privateServer, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([]);
    expect(result.current.privateInstallPageUrls).toEqual([]);
    expect(result.current.canReadPrivateUrls).toBe(false);
  });

  it("preserves cached private URLs while ingress state refetches", () => {
    hookState.isFetching = true;
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls(privateServer, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([
      "https://private.example.ts.net/mcp/platform-server",
    ]);
    expect(result.current.isLoading).toBe(false);
  });

  it("discards cached private URLs when the query fails", () => {
    hookState.isSuccess = false;
    hookState.isError = true;
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls(privateServer, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([]);
    expect(result.current.privateInstallPageUrls).toEqual([]);
    expect(result.current.isError).toBe(true);
  });

  it("discards cached private URLs after switching to public-only", () => {
    const { result } = renderHook(() =>
      usePrivateMcpServerUrls({ networkAccessMode: "public_only" }, endpoints),
    );

    expect(result.current.privateMcpUrls).toEqual([]);
    expect(result.current.privateInstallPageUrls).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });
});

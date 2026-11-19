import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogServer } from "@/pages/catalog/hooks";

const mocks = vi.hoisted(() => ({
  find: vi.fn(),
  hasScope: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
  useOrganization: () => ({
    id: "org-1",
    projects: [
      { id: "project-1", slug: "default" },
      { id: "project-2", slug: "other" },
    ],
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope, isLoading: false }),
}));
vi.mock("@gram/client/react-query/findShadowMCPInventoryServers.js", () => ({
  useFindShadowMCPInventoryServers: (...args: unknown[]) => mocks.find(...args),
}));

import { useShadowMCPReplacement } from "./useShadowMCPReplacement";

const SERVERS = [
  {
    registrySpecifier: "example/tools",
    remotes: [
      {
        url: "https://tools.example.com/mcp",
        transportType: "streamable-http",
      },
      {
        url: "https://linked.example.com/mcp",
        transportType: "streamable-http",
      },
      {
        url: "http://insecure.example.com/mcp",
        transportType: "streamable-http",
      },
    ],
  },
] as unknown as CatalogServer[];

function inventoryServer(name: string, official = false) {
  return {
    canonicalServerUrl: `https://${name}.example.com/mcp`,
    serverName: name,
    serverSlug: `${name}-example-com-mcp-12345678`,
    urlHost: `${name}.example.com`,
    userCount: 2,
    ...(official
      ? {
          officialServer: {
            mcpServerId: "mcp-1",
            name: "Linked",
            directAccessAllowed: false,
          },
        }
      : {}),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.hasScope.mockReturnValue(true);
  mocks.find.mockReturnValue({
    isLoading: false,
    data: {
      matches: [
        {
          serverUrl: "https://tools.example.com/mcp",
          server: inventoryServer("tools"),
        },
        {
          serverUrl: "https://linked.example.com/mcp",
          server: inventoryServer("linked", true),
        },
      ],
    },
  });
});

describe("useShadowMCPReplacement", () => {
  it("offers unlinked Shadow MCP servers that match, selected by default", () => {
    const { result } = renderHook(() =>
      useShadowMCPReplacement({ servers: SERVERS, enabled: true }),
    );

    // Only https endpoints are looked up: those are what the catalog adds.
    expect(mocks.find).toHaveBeenCalledWith(
      {
        projectId: "project-1",
        serverUrls: [
          "https://tools.example.com/mcp",
          "https://linked.example.com/mcp",
        ],
      },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
    // A server that already has an official version is left alone.
    expect(result.current.candidates).toEqual([
      {
        projectId: "project-1",
        remoteUrl: "https://tools.example.com/mcp",
        shadowServerUrl: "https://tools.example.com/mcp",
        shadowName: "tools",
        shadowServerSlug: "tools-example-com-mcp-12345678",
        userCount: 2,
      },
    ]);
    expect(result.current.selected).toEqual(result.current.candidates);

    act(() => result.current.setSelected(result.current.candidates[0]!, false));
    expect(result.current.selected).toEqual([]);

    act(() => result.current.reset());
    expect(result.current.selected).toHaveLength(1);
  });

  it("looks in the install's project", () => {
    renderHook(() =>
      useShadowMCPReplacement({
        servers: SERVERS,
        projectSlug: "other",
        enabled: true,
      }),
    );
    expect(mocks.find).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: "project-2" }),
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  it("offers nothing to people who cannot manage Shadow MCP servers", () => {
    mocks.hasScope.mockReturnValue(false);
    const { result } = renderHook(() =>
      useShadowMCPReplacement({ servers: SERVERS, enabled: true }),
    );
    expect(mocks.find).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(result.current.candidates).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });
});

import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityToast,
  mcpServerVisibilityUpdateForm,
} from "./mcp-server-visibility";

const mocks = vi.hoisted(() => ({
  invalidateAllMcpServers: vi.fn(() => Promise.resolve()),
  invalidateAllGetMcpServer: vi.fn(() => Promise.resolve()),
  invalidateAllMcpEndpoints: vi.fn(() => Promise.resolve()),
  invalidateAllPlugins: vi.fn(() => Promise.resolve()),
  invalidateAllPublishStatus: vi.fn(() => Promise.resolve()),
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  invalidateAllMcpServers: mocks.invalidateAllMcpServers,
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  invalidateAllGetMcpServer: mocks.invalidateAllGetMcpServer,
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  invalidateAllMcpEndpoints: mocks.invalidateAllMcpEndpoints,
}));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  invalidateAllPlugins: mocks.invalidateAllPlugins,
}));
vi.mock("@gram/client/react-query/publishStatus.js", () => ({
  invalidateAllPublishStatus: mocks.invalidateAllPublishStatus,
}));

function mcpServer(overrides: Partial<McpServer> = {}): McpServer {
  return {
    id: "srv_1",
    projectId: "proj_1",
    name: "Slack",
    slug: "slack",
    toolsetId: "ts_1",
    environmentId: "env_1",
    toolVariationsGroupId: "tvg_1",
    remoteMcpServerId: "remote_1",
    tunneledMcpServerId: "tunnel_1",
    unproxiedMcpServerId: "unproxied_1",
    userSessionIssuerId: "issuer_1",
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-02T00:00:00Z"),
    ...overrides,
  };
}

describe("mcpServerVisibilityUpdateForm", () => {
  it("preserves every existing field and sets the requested visibility", () => {
    const form = mcpServerVisibilityUpdateForm(mcpServer(), "disabled");

    expect(form).toEqual({
      id: "srv_1",
      name: "Slack",
      remoteMcpServerId: "remote_1",
      tunneledMcpServerId: "tunnel_1",
      toolsetId: "ts_1",
      unproxiedMcpServerId: "unproxied_1",
      environmentId: "env_1",
      toolVariationsGroupId: "tvg_1",
      visibility: "disabled",
    });
  });

  it("maps absent optional fields to undefined rather than null", () => {
    const form = mcpServerVisibilityUpdateForm(
      mcpServer({
        name: undefined,
        toolsetId: undefined,
        environmentId: undefined,
        toolVariationsGroupId: undefined,
        remoteMcpServerId: undefined,
        tunneledMcpServerId: undefined,
        unproxiedMcpServerId: undefined,
      }),
      "private",
    );

    expect(form.id).toBe("srv_1");
    expect(form.visibility).toBe("private");
    expect(form.name).toBeUndefined();
    expect(form.toolsetId).toBeUndefined();
    expect(form.environmentId).toBeUndefined();
    expect(form.toolVariationsGroupId).toBeUndefined();
    expect(form.remoteMcpServerId).toBeUndefined();
    expect(form.tunneledMcpServerId).toBeUndefined();
    expect(form.unproxiedMcpServerId).toBeUndefined();
  });

  it("does not leak fields the update endpoint does not accept", () => {
    const form = mcpServerVisibilityUpdateForm(mcpServer(), "public");

    expect(form).not.toHaveProperty("projectId");
    expect(form).not.toHaveProperty("slug");
    expect(form).not.toHaveProperty("userSessionIssuerId");
    expect(form).not.toHaveProperty("networkAccessMode");
    expect(form).not.toHaveProperty("createdAt");
    expect(form).not.toHaveProperty("updatedAt");
  });
});

describe("mcpServerVisibilityToast", () => {
  it.each([
    ["disabled", "MCP server disabled"],
    ["private", "MCP server enabled"],
    ["public", "MCP server set to public"],
  ] as const)("copies %s as %j", (visibility, copy) => {
    expect(mcpServerVisibilityToast(visibility)).toBe(copy);
  });

  it("falls back to a generic message for an unknown visibility", () => {
    expect(
      mcpServerVisibilityToast("something-new" as McpServer["visibility"]),
    ).toBe("MCP server updated");
  });
});

describe("invalidateMcpServerQueries", () => {
  beforeEach(() => {
    mocks.invalidateAllMcpServers.mockClear();
    mocks.invalidateAllGetMcpServer.mockClear();
    mocks.invalidateAllMcpEndpoints.mockClear();
    mocks.invalidateAllPlugins.mockClear();
    mocks.invalidateAllPublishStatus.mockClear();
  });

  it("invalidates the server list, detail, endpoint, plugin and publish-status queries", async () => {
    const queryClient = {} as QueryClient;

    await invalidateMcpServerQueries(queryClient);

    const filters = { refetchType: "all" };
    expect(mocks.invalidateAllMcpServers).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateAllMcpServers).toHaveBeenCalledWith(
      queryClient,
      filters,
    );
    expect(mocks.invalidateAllGetMcpServer).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateAllGetMcpServer).toHaveBeenCalledWith(
      queryClient,
      filters,
    );
    expect(mocks.invalidateAllMcpEndpoints).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateAllMcpEndpoints).toHaveBeenCalledWith(
      queryClient,
      filters,
    );
    // Enabling a server auto-attaches it to the Default plugin server-side,
    // so plugin membership and publish freshness must refresh with it.
    expect(mocks.invalidateAllPlugins).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateAllPlugins).toHaveBeenCalledWith(
      queryClient,
      filters,
    );
    expect(mocks.invalidateAllPublishStatus).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateAllPublishStatus).toHaveBeenCalledWith(
      queryClient,
      filters,
    );
  });

  it("waits for every invalidation before resolving", async () => {
    let settled = false;
    mocks.invalidateAllMcpEndpoints.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          setTimeout(() => {
            settled = true;
            resolve();
          }, 0);
        }),
    );

    await invalidateMcpServerQueries({} as QueryClient);

    expect(settled).toBe(true);
  });

  it("rejects when any invalidation rejects", async () => {
    mocks.invalidateAllGetMcpServer.mockImplementationOnce(() =>
      Promise.reject(new Error("boom")),
    );

    await expect(invalidateMcpServerQueries({} as QueryClient)).rejects.toThrow(
      "boom",
    );
  });
});

import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Toolset } from "@/lib/toolTypes";
import { useMcpServerAuthTarget, useToolsetAuthTarget } from "./authTarget";

const { get, update, setUserSessionIssuer } = vi.hoisted(() => ({
  get: vi.fn(),
  update: vi.fn(),
  setUserSessionIssuer: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    mcpServers: { get, update },
    toolsets: { setUserSessionIssuer },
  }),
}));

vi.mock("@/hooks/useToolsetUrl", () => ({
  useMcpUrl: () => ({
    url: "https://platform.example.com/mcp/hosted",
    customServerURL: undefined,
  }),
}));

describe("useMcpServerAuthTarget", () => {
  it("uses the MCP server resource and refreshes full-record fields before linking", async () => {
    const renderedServer: McpServer = {
      createdAt: new Date(0),
      id: "mcp-server-id",
      projectId: "project-id",
      slug: "server",
      networkAccessMode: "public_only",
      updatedAt: new Date(0),
      visibility: "private",
    };
    const latestServer: McpServer = {
      ...renderedServer,
      networkAccessMode: "private_only",
      remoteMcpServerId: "remote-id",
      visibility: "public",
    };
    get.mockResolvedValue(latestServer);
    update.mockResolvedValue(undefined);

    const { result } = renderHook(() => useMcpServerAuthTarget(renderedServer));

    expect(result.current.permissionResourceId).toBe("mcp-server-id");

    await act(() =>
      result.current.linkUserSessionIssuer?.("organization-issuer-id"),
    );

    expect(get).toHaveBeenCalledWith(
      { id: "mcp-server-id" },
      undefined,
      undefined,
    );
    expect(update).toHaveBeenCalledWith(
      {
        updateMcpServerForm: expect.objectContaining({
          id: "mcp-server-id",
          networkAccessMode: "private_only",
          remoteMcpServerId: "remote-id",
          userSessionIssuerId: "organization-issuer-id",
          visibility: "public",
        }),
      },
      undefined,
      undefined,
    );
  });

  it("uses the backing toolset as the permission resource", () => {
    const server: McpServer = {
      createdAt: new Date(0),
      id: "mcp-server-id",
      networkAccessMode: "public_only",
      projectId: "project-id",
      toolsetId: "toolset-id",
      updatedAt: new Date(0),
      visibility: "private",
    };

    const { result } = renderHook(() => useMcpServerAuthTarget(server));

    expect(result.current.permissionResourceId).toBe("toolset-id");
    expect(result.current.mcpServerId).toBe("mcp-server-id");
  });
});

describe("useToolsetAuthTarget", () => {
  it("links the issuer on the toolset's canonical MCP server", async () => {
    const canonical: McpServer = {
      createdAt: new Date(0),
      id: "toolset-id",
      networkAccessMode: "public_only",
      projectId: "project-id",
      toolsetId: "toolset-id",
      toolVariationsGroupId: "variation-group-id",
      updatedAt: new Date(0),
      visibility: "private",
    };
    get.mockResolvedValue(canonical);
    update.mockResolvedValue(undefined);
    setUserSessionIssuer.mockReset();
    const toolset = {
      id: "toolset-id",
      slug: "hosted",
      projectId: "project-id",
      mcpSlug: "hosted",
    } as Toolset;

    const { result } = renderHook(() => useToolsetAuthTarget(toolset));

    await act(() => result.current.linkUserSessionIssuer?.("issuer-id"));

    expect(get).toHaveBeenCalledWith(
      { id: "toolset-id" },
      undefined,
      undefined,
    );
    expect(update).toHaveBeenCalledWith(
      {
        updateMcpServerForm: expect.objectContaining({
          id: "toolset-id",
          toolsetId: "toolset-id",
          toolVariationsGroupId: "variation-group-id",
          userSessionIssuerId: "issuer-id",
          visibility: "private",
        }),
      },
      undefined,
      undefined,
    );
    expect(setUserSessionIssuer).not.toHaveBeenCalled();
  });
});

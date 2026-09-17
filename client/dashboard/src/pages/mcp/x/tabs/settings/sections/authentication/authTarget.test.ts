import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useMcpServerAuthTarget } from "./authTarget";

const { get, update } = vi.hoisted(() => ({
  get: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    mcpServers: { get, update },
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

    expect(get).toHaveBeenCalledWith({ id: "mcp-server-id" });
    expect(update).toHaveBeenCalledWith({
      updateMcpServerForm: expect.objectContaining({
        id: "mcp-server-id",
        networkAccessMode: "private_only",
        remoteMcpServerId: "remote-id",
        userSessionIssuerId: "organization-issuer-id",
        visibility: "public",
      }),
    });
  });
});

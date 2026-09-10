import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { McpServerCardStatus } from "./mcp-server-x-sidebar-nav";

vi.mock("@/pages/mcp/x/MCPServerDetails", () => ({
  default: () => null,
  MCPServerAvailabilityToggle: () => (
    <button role="switch" aria-label="Server availability" />
  ),
  MCPServerStatusDropdown: () => <button>Private</button>,
}));

function server(overrides: Partial<McpServer>): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } as McpServer;
}

afterEach(cleanup);

describe("McpServerCardStatus", () => {
  it("renders an unlabeled availability switch for Remote MCP", () => {
    render(
      <McpServerCardStatus
        server={server({ remoteMcpServerId: "remote-source-1" })}
      />,
    );

    expect(
      screen.getByRole("switch", { name: "Server availability" }),
    ).toBeDefined();
    expect(screen.queryByText("Visibility")).toBeNull();
    expect(screen.queryByRole("button", { name: "Private" })).toBeNull();
  });

  it("preserves the labeled visibility dropdown for tunneled MCP", () => {
    render(
      <McpServerCardStatus
        server={server({ tunneledMcpServerId: "tunneled-source-1" })}
      />,
    );

    expect(screen.getByText("Visibility")).toBeDefined();
    expect(screen.getByRole("button", { name: "Private" })).toBeDefined();
    expect(screen.queryByRole("switch")).toBeNull();
  });
});

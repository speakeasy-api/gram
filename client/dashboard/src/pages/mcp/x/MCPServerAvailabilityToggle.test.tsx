import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MCPServerAvailabilityToggle,
  MCPServerStatusDropdown,
} from "./MCPServerDetails";

const mocks = vi.hoisted(() => ({
  hasScope: vi.fn(),
  mutate: vi.fn(),
  tunneledSource: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({
    isPending: false,
    mutate: mocks.mutate,
  }),
}));
vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: () => mocks.tunneledSource(),
}));
vi.mock("@/components/ui/Dropdown", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    disabled,
  }: {
    children: ReactNode;
    disabled?: boolean;
  }) => (
    <div role="menuitem" data-disabled={disabled ? "" : undefined}>
      {children}
    </div>
  ),
}));

const server = {
  id: "mcp-server-1",
  projectId: "project-1",
  name: "Example server",
  networkAccessMode: "public_only",
  remoteMcpServerId: "remote-source-1",
  toolVariationsGroupId: "tool-filter-1",
  visibility: "private",
  createdAt: new Date(0),
  updatedAt: new Date(0),
} as McpServer;

function renderToggle(mcpServer: McpServer = server): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MCPServerAvailabilityToggle server={mcpServer} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mocks.hasScope.mockReturnValue(true);
  mocks.tunneledSource.mockReturnValue({ data: { allowPublic: false } });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("MCPServerAvailabilityToggle", () => {
  it("disables a Remote MCP server while preserving its backend fields", () => {
    renderToggle();

    fireEvent.click(screen.getByRole("switch", { name: "Disable MCP server" }));

    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: {
          id: "mcp-server-1",
          name: "Example server",
          remoteMcpServerId: "remote-source-1",
          tunneledMcpServerId: undefined,
          toolsetId: undefined,
          unproxiedMcpServerId: undefined,
          environmentId: undefined,
          toolVariationsGroupId: "tool-filter-1",
          visibility: "disabled",
        },
      },
    });
  });

  it("enables a disabled server as private", () => {
    renderToggle({ ...server, visibility: "disabled" });

    fireEvent.click(screen.getByRole("switch", { name: "Enable MCP server" }));

    expect(mocks.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateMcpServerForm: expect.objectContaining({
            visibility: "private",
          }),
        }),
      }),
    );
  });

  it("preserves the existing write-scope gate", () => {
    mocks.hasScope.mockReturnValue(false);
    renderToggle();

    expect(
      (
        screen.getByRole("switch", {
          name: "Disable MCP server",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("keeps Public blocked until a tunneled source opts in", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MCPServerStatusDropdown
          server={{
            ...server,
            remoteMcpServerId: undefined,
            tunneledMcpServerId: "tunneled-source-1",
          }}
        />
      </QueryClientProvider>,
    );

    expect(
      screen.getByText(
        "Enable public access on the tunnel source first to allow anonymous serving.",
      ),
    ).toBeDefined();
    expect(
      screen
        .getByRole("menuitem", { name: /Public/i })
        .getAttribute("data-disabled"),
    ).not.toBeNull();
  });
});

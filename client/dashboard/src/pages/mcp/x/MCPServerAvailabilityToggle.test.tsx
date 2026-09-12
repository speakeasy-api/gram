import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MCPServerAvailabilityToggle,
  MCPServerStatusDropdown,
} from "./MCPServerDetails";

const mocks = vi.hoisted(() => ({
  hasScope: vi.fn(),
  mutate: vi.fn(),
  getServer: vi.fn(),
  tunneledSource: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ mcpServers: { get: mocks.getServer } }),
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  buildGetMcpServerQuery: (_client: unknown, request: { id: string }) => ({
    queryKey: ["mcp-server", request.id],
    queryFn: () => mocks.getServer(request),
  }),
  invalidateAllGetMcpServer: vi.fn(),
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
    onSelect,
  }: {
    children: ReactNode;
    disabled?: boolean;
    onSelect?: () => void;
  }) => (
    <div
      role="menuitem"
      data-disabled={disabled ? "" : undefined}
      onClick={disabled ? undefined : onSelect}
    >
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
  mocks.getServer.mockResolvedValue(server);
  mocks.tunneledSource.mockReturnValue({ data: { allowPublic: false } });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("MCPServerAvailabilityToggle", () => {
  it("fetches the latest Remote MCP server before preserving its backend fields", async () => {
    mocks.getServer.mockResolvedValue({
      ...server,
      name: "Latest name",
      toolVariationsGroupId: "latest-tool-filter",
    });
    renderToggle();

    fireEvent.click(screen.getByRole("switch", { name: "Disable MCP server" }));

    await waitFor(() =>
      expect(mocks.getServer).toHaveBeenCalledWith({ id: "mcp-server-1" }),
    );
    await waitFor(() =>
      expect(mocks.mutate).toHaveBeenCalledWith({
        request: {
          updateMcpServerForm: {
            id: "mcp-server-1",
            name: "Latest name",
            remoteMcpServerId: "remote-source-1",
            tunneledMcpServerId: undefined,
            toolsetId: undefined,
            unproxiedMcpServerId: undefined,
            environmentId: undefined,
            toolVariationsGroupId: "latest-tool-filter",
            visibility: "disabled",
          },
        },
      }),
    );
  });

  it("enables a disabled server as private", async () => {
    const disabledServer = { ...server, visibility: "disabled" as const };
    mocks.getServer.mockResolvedValue(disabledServer);
    renderToggle(disabledServer);

    fireEvent.click(screen.getByRole("switch", { name: "Enable MCP server" }));

    await waitFor(() =>
      expect(mocks.mutate).toHaveBeenCalledWith(
        expect.objectContaining({
          request: expect.objectContaining({
            updateMcpServerForm: expect.objectContaining({
              visibility: "private",
            }),
          }),
        }),
      ),
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
    expect(mocks.hasScope).toHaveBeenCalledWith("mcp:write", "mcp-server-1");
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

  it("keeps the tunneled Public option after the source opts in", async () => {
    mocks.tunneledSource.mockReturnValue({ data: { allowPublic: true } });
    const tunneledServer = {
      ...server,
      remoteMcpServerId: undefined,
      tunneledMcpServerId: "tunneled-source-1",
    };
    mocks.getServer.mockResolvedValue(tunneledServer);
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MCPServerStatusDropdown server={tunneledServer} />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("menuitem", { name: /Public/i }));

    await waitFor(() =>
      expect(mocks.mutate).toHaveBeenCalledWith({
        request: {
          updateMcpServerForm: expect.objectContaining({
            tunneledMcpServerId: "tunneled-source-1",
            visibility: "public",
          }),
        },
      }),
    );
  });
});

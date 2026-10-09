import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MCPServerAvailabilityToggle,
  MCPServerStatusDropdown,
} from "./MCPServerDetails";

const mocks = vi.hoisted(() => ({
  hasScope: vi.fn(),
  mutate: vi.fn(),
  mutationOptions: vi.fn(),
  invalidateAudience: vi.fn(() => Promise.resolve()),
  tunneledSource: vi.fn(),
}));

vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: mocks.invalidateAudience,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: (options: unknown) => {
    mocks.mutationOptions(options);
    return { isPending: false, mutate: mocks.mutate };
  },
}));
vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: () => mocks.tunneledSource(),
}));
vi.mock("@/components/mcp/use-shared-tunnel-impact", async (original) => ({
  ...(await original<
    typeof import("@/components/mcp/use-shared-tunnel-impact")
  >()),
  useSharedTunnelImpact: () => ({
    servers: [
      { id: "mcp-server-1", name: "Example server", visibility: "private" },
      { id: "sibling", name: "Sibling server", visibility: "private" },
    ],
    isReady: true,
    isLoading: false,
    isError: false,
    retry: () => {},
  }),
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

// The status dropdown links to the source's public-access section, so these
// renders need a router as well as the query client.
function renderInApp(ui: ReactNode): void {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>
    </MemoryRouter>,
  );
}

function renderToggle(mcpServer: McpServer = server): void {
  renderInApp(<MCPServerAvailabilityToggle server={mcpServer} />);
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

    expect(mocks.mutate).toHaveBeenCalledWith(
      {
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
      },
      expect.anything(),
    );
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
      expect.anything(),
    );
  });

  it.each(["private", "disabled"] as const)(
    "refreshes team audience after changing visibility from %s",
    async (visibility) => {
      renderToggle({ ...server, visibility });
      fireEvent.click(screen.getByRole("switch"));
      expect(mocks.invalidateAudience).not.toHaveBeenCalled();

      const [variables] = mocks.mutate.mock.calls.at(-1)!;
      const [options] = mocks.mutationOptions.mock.calls.at(-1)!;
      await options.onSuccess(undefined, variables);

      expect(mocks.invalidateAudience).toHaveBeenCalledExactlyOnceWith(
        expect.any(QueryClient),
        { refetchType: "all" },
      );
    },
  );

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
    renderInApp(
      <MCPServerStatusDropdown
        server={{
          ...server,
          remoteMcpServerId: undefined,
          tunneledMcpServerId: "tunneled-source-1",
        }}
      />,
    );

    expect(
      screen.getByText(
        "Enable public access on the tunnel source first to allow anonymous serving.",
      ),
    ).toBeDefined();
    expect(
      screen
        .getByRole("menuitem", { name: /^Public/ })
        .getAttribute("data-disabled"),
    ).not.toBeNull();
  });

  it("confirms a tunneled server going public against its tunnel's servers", () => {
    mocks.tunneledSource.mockReturnValue({ data: { allowPublic: true } });
    renderInApp(
      <MCPServerStatusDropdown
        server={{
          ...server,
          remoteMcpServerId: undefined,
          tunneledMcpServerId: "tunneled-source-1",
        }}
      />,
    );

    fireEvent.click(screen.getByRole("menuitem", { name: /^Public/ }));

    // Nothing changes until the shared-tunnel warning is confirmed.
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByText("Sibling server")).toBeTruthy();
    expect(screen.getByText(/bypass per-tool access control/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Make public" }));

    expect(mocks.mutate).toHaveBeenCalledWith(
      {
        request: {
          updateMcpServerForm: expect.objectContaining({
            tunneledMcpServerId: "tunneled-source-1",
            visibility: "public",
          }),
        },
      },
      expect.anything(),
    );

    // The dialog stays open, showing Saving, until the change lands.
    expect(screen.getByText("Make this MCP server public?")).toBeTruthy();
    const options = mocks.mutate.mock.calls[0]?.[1] as {
      onSuccess: () => void;
    };
    act(() => options.onSuccess());
    expect(screen.queryByText("Make this MCP server public?")).toBeNull();
  });

  it("skips the confirmation when a tunneled server is already public", () => {
    mocks.tunneledSource.mockReturnValue({ data: { allowPublic: true } });
    renderInApp(
      <MCPServerStatusDropdown
        server={{
          ...server,
          remoteMcpServerId: undefined,
          tunneledMcpServerId: "tunneled-source-1",
          visibility: "public",
        }}
      />,
    );

    fireEvent.click(screen.getByRole("menuitem", { name: /^Public/ }));

    expect(screen.queryByText("Make this MCP server public?")).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});

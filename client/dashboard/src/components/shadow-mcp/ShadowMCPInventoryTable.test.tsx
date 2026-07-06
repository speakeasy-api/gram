import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { ShadowMCPInventoryTable } from "./ShadowMCPInventoryTable";

const mocks = vi.hoisted(() => ({
  useShadowMCPInventory: vi.fn(),
  invalidateShadowMCPInventory: vi.fn(),
  allowInventoryServerMutation: vi.fn(),
  blockInventoryServerMutation: vi.fn(),
  clearInventoryServerMutation: vi.fn(),
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: mocks.invalidateShadowMCPInventory,
  useShadowMCPInventory: mocks.useShadowMCPInventory,
}));

vi.mock("@gram/client/react-query/allowShadowMCPInventoryServer.js", () => ({
  useAllowShadowMCPInventoryServerMutation: mocks.allowInventoryServerMutation,
}));

vi.mock("@gram/client/react-query/blockShadowMCPInventoryServer.js", () => ({
  useBlockShadowMCPInventoryServerMutation: mocks.blockInventoryServerMutation,
}));

vi.mock(
  "@gram/client/react-query/clearShadowMCPInventoryServerAccess.js",
  () => ({
    useClearShadowMCPInventoryServerAccessMutation:
      mocks.clearInventoryServerMutation,
  }),
);

vi.mock("@speakeasy-api/moonshine", () => ({
  Badge: Object.assign(
    ({ children }: { children: ReactNode }) => <span>{children}</span>,
    {
      Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
    },
  ),
  Button: Object.assign(
    ({
      children,
      disabled,
      onClick,
    }: {
      children: ReactNode;
      disabled?: boolean;
      onClick?: () => void;
    }) => (
      <button disabled={disabled} onClick={onClick}>
        {children}
      </button>
    ),
    {
      Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
    },
  ),
  Table: ({
    columns,
    data,
    rowKey,
  }: {
    columns: Array<{
      header: ReactNode;
      key: string;
      render?: (row: ShadowMCPInventoryServer) => ReactNode;
    }>;
    data: ShadowMCPInventoryServer[];
    rowKey: (row: ShadowMCPInventoryServer) => string;
  }) => (
    <table>
      <thead>
        <tr>
          {columns.map((column) => (
            <th key={column.key}>{column.header}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {data.map((row) => (
          <tr key={rowKey(row)}>
            {columns.map((column) => (
              <td key={column.key}>{column.render?.(row)}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  ),
}));

function inventoryServer(
  overrides: Partial<ShadowMCPInventoryServer> & {
    canonicalServerUrl: string;
  },
): ShadowMCPInventoryServer {
  const { canonicalServerUrl, ...rest } = overrides;

  return {
    access: "none",
    canonicalServerUrl,
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastCalled: undefined,
    lastSeen: new Date("2026-01-02T10:00:00Z"),
    observedUseCount: 0,
    rule: undefined,
    serverName: undefined,
    topUsers: [],
    urlHost: new URL(canonicalServerUrl).host,
    userCount: 0,
    ...rest,
  };
}

function renderInventoryTable(projectID = "project-id-1") {
  const queryClient = new QueryClient();

  return render(
    <QueryClientProvider client={queryClient}>
      <ShadowMCPInventoryTable projectID={projectID} />
    </QueryClientProvider>,
  );
}

function mockShadowMCPInventory({
  servers = [],
  isLoading = false,
  error = null,
  nextCursor,
}: {
  servers?: ShadowMCPInventoryServer[];
  isLoading?: boolean;
  error?: Error | null;
  nextCursor?: string;
} = {}) {
  mocks.useShadowMCPInventory.mockReturnValue({
    data: { servers, nextCursor },
    isLoading,
    error,
  });
}

describe("ShadowMCPInventoryTable", () => {
  beforeEach(() => {
    for (const mock of Object.values(mocks)) {
      mock.mockReset();
    }

    mockShadowMCPInventory();
    mocks.allowInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn(),
    });
    mocks.blockInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn(),
    });
    mocks.clearInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn(),
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("renders Shadow MCP inventory rows", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "allowed",
          canonicalServerUrl: "https://github.example.com/mcp",
          lastCalled: new Date("2026-01-02T11:30:00Z"),
          observedUseCount: 42,
          serverName: "GitHub MCP",
          userCount: 3,
        }),
        inventoryServer({
          canonicalServerUrl: "https://unused.example.com/mcp",
          lastCalled: undefined,
          lastSeen: new Date("2026-01-03T11:30:00Z"),
        }),
      ],
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("GitHub MCP")).toBeTruthy();
    });
    expect(screen.getByText("https://github.example.com/mcp")).toBeTruthy();
    expect(screen.getByText("Allowed")).toBeTruthy();
    expect(screen.getByText("42 calls")).toBeTruthy();
    expect(screen.getByText("3 users")).toBeTruthy();
    expect(screen.getByText("Never")).toBeTruthy();
    expect(screen.getByText("https://unused.example.com/mcp")).toBeTruthy();
    expect(mocks.useShadowMCPInventory).toHaveBeenCalledWith(
      { projectId: "project-id-1", limit: 50 },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  it("allows, blocks, and clears Shadow MCP inventory URL access", async () => {
    const allowServer = vi.fn().mockResolvedValue({});
    const blockServer = vi.fn().mockResolvedValue({});
    const clearServer = vi.fn().mockResolvedValue({});
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://pending.example.com/mcp",
          serverName: "Pending MCP",
        }),
        inventoryServer({
          access: "allowed",
          canonicalServerUrl: "https://allowed.example.com/mcp",
          serverName: "Allowed MCP",
        }),
        inventoryServer({
          access: "denied",
          canonicalServerUrl: "https://blocked.example.com/mcp",
          serverName: "Blocked MCP",
        }),
      ],
    });
    mocks.allowInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: allowServer,
    });
    mocks.blockInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: blockServer,
    });
    mocks.clearInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: clearServer,
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("Pending MCP")).toBeTruthy();
    });

    const pendingRow = screen.getByText("Pending MCP").closest("tr");
    const allowedRow = screen.getByText("Allowed MCP").closest("tr");
    const blockedRow = screen.getByText("Blocked MCP").closest("tr");
    if (!pendingRow || !allowedRow || !blockedRow) {
      throw new Error("Inventory rows not found");
    }

    fireEvent.click(within(pendingRow).getByRole("button", { name: "Allow" }));
    fireEvent.click(within(allowedRow).getByRole("button", { name: "Block" }));
    fireEvent.click(within(blockedRow).getByRole("button", { name: "Clear" }));

    await waitFor(() => {
      expect(allowServer).toHaveBeenCalledWith({
        request: {
          shadowMCPInventoryServerAccessForm: {
            projectId: "project-id-1",
            serverName: "Pending MCP",
            serverUrl: "https://pending.example.com/mcp",
          },
        },
      });
      expect(blockServer).toHaveBeenCalledWith({
        request: {
          shadowMCPInventoryServerAccessForm: {
            projectId: "project-id-1",
            serverName: "Allowed MCP",
            serverUrl: "https://allowed.example.com/mcp",
          },
        },
      });
      expect(clearServer).toHaveBeenCalledWith({
        request: {
          clearShadowMCPInventoryServerAccessRequestBody: {
            projectId: "project-id-1",
            serverUrl: "https://blocked.example.com/mcp",
          },
        },
      });
    });
    expect(mocks.invalidateShadowMCPInventory).toHaveBeenCalled();
  });
});

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
  clearInventoryServerMutation: vi.fn(),
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: mocks.invalidateShadowMCPInventory,
  useShadowMCPInventory: mocks.useShadowMCPInventory,
}));

vi.mock("@gram/client/react-query/allowShadowMCPInventoryServer.js", () => ({
  useAllowShadowMCPInventoryServerMutation: mocks.allowInventoryServerMutation,
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
  Table: Object.assign(
    ({ children }: { children: ReactNode }) => <table>{children}</table>,
    {
      Header: ({
        columns,
        onSortChange,
        sort,
      }: {
        columns: Array<{
          header: ReactNode;
          id?: string;
          key: string;
          sortable?: boolean;
        }>;
        onSortChange?: (sort: {
          id: string;
          direction: "asc" | "desc";
        }) => void;
        sort?: { id: string; direction: "asc" | "desc" } | null;
      }) => (
        <thead>
          <tr>
            {columns.map((column) => {
              const columnID = column.id ?? column.key;
              return (
                <th key={column.key}>
                  {column.sortable ? (
                    <button
                      onClick={() => {
                        onSortChange?.({
                          id: columnID,
                          direction:
                            sort?.id === columnID && sort.direction === "asc"
                              ? "desc"
                              : "asc",
                        });
                      }}
                    >
                      {column.header}
                    </button>
                  ) : (
                    column.header
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
      ),
      Body: ({
        columns,
        data,
        rowKey,
      }: {
        columns: Array<{
          key: string;
          render?: (row: ShadowMCPInventoryServer) => ReactNode;
        }>;
        data: ShadowMCPInventoryServer[];
        rowKey: (row: ShadowMCPInventoryServer) => string;
      }) => (
        <tbody>
          {data.map((row) => (
            <tr key={rowKey(row)}>
              {columns.map((column) => (
                <td key={column.key}>{column.render?.(row)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      ),
    },
  ),
  sortTableData: (
    data: ShadowMCPInventoryServer[],
    columns: Array<{
      id?: string;
      key: string;
      sortValue?: (row: ShadowMCPInventoryServer) => number | string;
    }>,
    sort?: { id: string; direction: "asc" | "desc" } | null,
  ) => {
    if (!sort) return data;
    const column = columns.find((c) => (c.id ?? c.key) === sort.id);
    if (!column?.sortValue) return data;

    return data.slice().sort((a, b) => {
      const av = column.sortValue!(a);
      const bv = column.sortValue!(b);
      const comparison =
        typeof av === "number" && typeof bv === "number"
          ? av - bv
          : String(av).localeCompare(String(bv));
      return sort.direction === "asc" ? comparison : -comparison;
    });
  },
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

function renderInventoryTable(
  projectID = "project-id-1",
  policyState: "blocking" | "flagging" | "none" | "unavailable" = "blocking",
) {
  const queryClient = new QueryClient();

  return render(
    <QueryClientProvider client={queryClient}>
      <ShadowMCPInventoryTable
        policyState={policyState}
        projectID={projectID}
      />
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
    expect(screen.getByText("Allowed by URL rule")).toBeTruthy();
    expect(screen.getByText("Blocked")).toBeTruthy();
    expect(screen.getByText("Blocked by policy")).toBeTruthy();
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

  it("sorts inventory columns and uses call count for Usage", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "allowed",
          canonicalServerUrl: "https://alpha.example.com/mcp",
          lastCalled: new Date("2026-01-03T11:30:00Z"),
          lastSeen: new Date("2026-01-03T11:30:00Z"),
          observedUseCount: 20,
          serverName: "Alpha MCP",
          userCount: 1,
        }),
        inventoryServer({
          canonicalServerUrl: "https://beta.example.com/mcp",
          lastCalled: new Date("2026-01-02T11:30:00Z"),
          lastSeen: new Date("2026-01-04T11:30:00Z"),
          observedUseCount: 1,
          serverName: "Beta MCP",
          userCount: 99,
        }),
      ],
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("Beta MCP")).toBeTruthy();
    });

    for (const header of [
      "Server",
      "Status",
      "Last called",
      "Last seen",
      "Usage",
    ]) {
      expect(screen.getByRole("button", { name: header })).toBeTruthy();
    }

    let rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]!).getByText("Beta MCP")).toBeTruthy();
    expect(within(rows[1]!).getByText("Alpha MCP")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Usage" }));

    rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]!).getByText("Beta MCP")).toBeTruthy();
    expect(within(rows[0]!).getByText("1 call")).toBeTruthy();
    expect(within(rows[0]!).getByText("99 users")).toBeTruthy();
    expect(within(rows[1]!).getByText("Alpha MCP")).toBeTruthy();
    expect(within(rows[1]!).getByText("20 calls")).toBeTruthy();
    expect(within(rows[1]!).getByText("1 user")).toBeTruthy();
  });

  it("adds and removes Shadow MCP inventory URL allow rules", async () => {
    const allowServer = vi.fn().mockResolvedValue({});
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
      ],
    });
    mocks.allowInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: allowServer,
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
    if (!pendingRow || !allowedRow) {
      throw new Error("Inventory rows not found");
    }

    fireEvent.click(
      within(pendingRow).getByRole("button", { name: "Add Allow Rule" }),
    );
    fireEvent.click(
      within(allowedRow).getByRole("button", { name: "Remove Allow Rule" }),
    );

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
      expect(clearServer).toHaveBeenCalledWith({
        request: {
          clearShadowMCPInventoryServerAccessRequestBody: {
            projectId: "project-id-1",
            serverUrl: "https://allowed.example.com/mcp",
          },
        },
      });
    });
    expect(mocks.invalidateShadowMCPInventory).toHaveBeenCalled();
  });

  it("renders observed status when blocking is inactive", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://observed.example.com/mcp",
          serverName: "Observed MCP",
        }),
      ],
    });

    renderInventoryTable("project-id-1", "flagging");

    await waitFor(() => {
      expect(screen.getByText("Observed MCP")).toBeTruthy();
    });
    expect(screen.getByText("Observed")).toBeTruthy();
    expect(screen.getByText("Not blocking")).toBeTruthy();
  });
});

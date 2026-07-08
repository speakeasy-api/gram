import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cloneElement,
  isValidElement,
  type ReactElement,
  type ReactNode,
} from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RiskPolicyBypassRequest } from "@gram/client/models/components/riskpolicybypassrequest.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { ShadowMCPInventoryTable } from "./ShadowMCPInventoryTable";

const mocks = vi.hoisted(() => ({
  useShadowMCPInventory: vi.fn(),
  usePolicyBypassRequests: vi.fn(),
  invalidateShadowMCPInventory: vi.fn(),
  allowInventoryServerMutation: vi.fn(),
  clearInventoryServerMutation: vi.fn(),
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: mocks.invalidateShadowMCPInventory,
  useShadowMCPInventory: mocks.useShadowMCPInventory,
}));

vi.mock("@gram/client/react-query/riskListPolicyBypassRequests.js", () => ({
  useRiskListPolicyBypassRequests: mocks.usePolicyBypassRequests,
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
      ...props
    }: {
      children: ReactNode;
      disabled?: boolean;
      onClick?: () => void;
      [key: string]: unknown;
    }) => (
      <button disabled={disabled} onClick={onClick} {...props}>
        {children}
      </button>
    ),
    {
      LeftIcon: ({ children }: { children: ReactNode }) => (
        <span>{children}</span>
      ),
      Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
    },
  ),
  Icon: ({ className }: { className?: string; name: string }) => (
    <span className={className} />
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
        handleLoadMore,
        hasMore,
        isLoading,
        rowKey,
      }: {
        columns: Array<{
          key: string;
          render?: (row: ShadowMCPInventoryServer) => ReactNode;
        }>;
        data: ShadowMCPInventoryServer[];
        handleLoadMore?: () => void;
        hasMore?: boolean;
        isLoading?: boolean;
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
          {hasMore && handleLoadMore ? (
            <tr>
              <td colSpan={columns.length}>
                <button onClick={handleLoadMore}>
                  {isLoading ? "Loading" : "Load more"}
                </button>
              </td>
            </tr>
          ) : null}
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

vi.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
  TooltipTrigger: ({
    asChild,
    children,
  }: {
    asChild?: boolean;
    children: ReactNode;
  }) => {
    if (asChild && isValidElement(children)) {
      return cloneElement(children as ReactElement<Record<string, unknown>>, {
        "data-tooltip-trigger": "true",
      });
    }

    return <>{children}</>;
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

function policyBypassRequest(
  overrides: Partial<RiskPolicyBypassRequest> & { id: string },
): RiskPolicyBypassRequest {
  return {
    createdAt: new Date("2026-01-01T10:00:00Z"),
    decidedAt: undefined,
    decidedBy: undefined,
    grantedPrincipalUrns: [],
    note: undefined,
    policyId: "policy-1",
    requesterEmail: "dev@example.com",
    requesterUserId: "user-1",
    status: "requested",
    targetDimensions: {},
    targetKey: undefined,
    targetKind: "shadow_mcp_server",
    targetLabel: undefined,
    updatedAt: new Date("2026-01-01T10:00:00Z"),
    ...overrides,
  };
}

function renderInventoryTable(
  projectID = "project-id-1",
  policyState: "blocking" | "flagging" | "none" | "unavailable" = "blocking",
  projectSlug = "project-slug-1",
) {
  const queryClient = new QueryClient();

  return render(
    <QueryClientProvider client={queryClient}>
      <ShadowMCPInventoryTable
        policyState={policyState}
        projectID={projectID}
        projectSlug={projectSlug}
      />
    </QueryClientProvider>,
  );
}

function mockShadowMCPInventory({
  servers = [],
  isLoading = false,
  isFetching = false,
  error = null,
  nextCursor,
}: {
  servers?: ShadowMCPInventoryServer[];
  isLoading?: boolean;
  isFetching?: boolean;
  error?: Error | null;
  nextCursor?: string;
} = {}) {
  mocks.useShadowMCPInventory.mockReturnValue({
    data: { servers, nextCursor },
    isFetching,
    isLoading,
    refetch: vi.fn(),
    error,
  });
}

describe("ShadowMCPInventoryTable", () => {
  beforeEach(() => {
    for (const mock of Object.values(mocks)) {
      mock.mockReset();
    }

    mockShadowMCPInventory();
    mocks.usePolicyBypassRequests.mockReturnValue({
      data: { requests: [] },
      isFetching: false,
      isLoading: false,
      error: null,
    });
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
    expect(mocks.usePolicyBypassRequests).toHaveBeenCalledWith(
      { gramProject: "project-slug-1" },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  it("shows request counts for each Shadow MCP server URL", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          canonicalServerUrl: "https://github.example.com/mcp",
          serverName: "GitHub MCP",
        }),
        inventoryServer({
          canonicalServerUrl: "https://slack.example.com/mcp",
          serverName: "Slack MCP",
        }),
      ],
    });
    mocks.usePolicyBypassRequests.mockReturnValue({
      data: {
        requests: [
          policyBypassRequest({
            id: "request-1",
            targetDimensions: {
              server_url: "https://github.example.com/mcp",
            },
          }),
          policyBypassRequest({
            id: "request-2",
            status: "denied",
            targetDimensions: {
              server_url: "https://github.example.com/mcp",
            },
          }),
          policyBypassRequest({
            id: "non-shadow-request",
            targetDimensions: {
              server_url: "https://github.example.com/mcp",
            },
            targetKind: "deployment",
          }),
        ],
      },
      isFetching: false,
      isLoading: false,
      error: null,
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("GitHub MCP")).toBeTruthy();
    });

    const githubRow = screen.getByText("GitHub MCP").closest("tr");
    const slackRow = screen.getByText("Slack MCP").closest("tr");
    if (!githubRow || !slackRow) {
      throw new Error("Inventory rows not found");
    }

    expect(within(githubRow).getByText("2")).toBeTruthy();
    expect(within(slackRow).getByText("0")).toBeTruthy();
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
    expect(within(rows[0]!).getByText("Alpha MCP")).toBeTruthy();
    expect(within(rows[1]!).getByText("Beta MCP")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Usage" }));

    rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]!).getByText("Beta MCP")).toBeTruthy();
    expect(within(rows[0]!).getByText("1 call")).toBeTruthy();
    expect(within(rows[0]!).getByText("99 users")).toBeTruthy();
    expect(within(rows[1]!).getByText("Alpha MCP")).toBeTruthy();
    expect(within(rows[1]!).getByText("20 calls")).toBeTruthy();
    expect(within(rows[1]!).getByText("1 user")).toBeTruthy();
  });

  it("loads more inventory rows through the Moonshine table", async () => {
    const firstPageServer = inventoryServer({
      canonicalServerUrl: "https://first.example.com/mcp",
      serverName: "First MCP",
    });
    const secondPageServer = inventoryServer({
      canonicalServerUrl: "https://second.example.com/mcp",
      serverName: "Second MCP",
    });
    const firstPageResponse = {
      data: { servers: [firstPageServer], nextCursor: "next-page" },
      error: null,
      isFetching: false,
      isLoading: false,
    };
    const secondPageResponse = {
      data: { servers: [secondPageServer], nextCursor: undefined },
      error: null,
      isFetching: false,
      isLoading: false,
    };
    mocks.useShadowMCPInventory.mockImplementation(
      (request: { cursor?: string }) => {
        if (request.cursor === "next-page") {
          return secondPageResponse;
        }

        return firstPageResponse;
      },
    );

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("First MCP")).toBeTruthy();
    });
    expect(screen.queryByText("Second MCP")).toBeFalsy();

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    await waitFor(() => {
      expect(screen.getByText("Second MCP")).toBeTruthy();
    });
    expect(mocks.useShadowMCPInventory).toHaveBeenCalledWith(
      { projectId: "project-id-1", limit: 50, cursor: "next-page" },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  it("keeps loaded inventory rows visible while loading the next page", async () => {
    const firstPageServer = inventoryServer({
      canonicalServerUrl: "https://first.example.com/mcp",
      serverName: "First MCP",
    });
    const firstPageResponse = {
      data: { servers: [firstPageServer], nextCursor: "next-page" },
      error: null,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    };
    const loadingNextPageResponse = {
      data: undefined,
      error: null,
      isFetching: true,
      isLoading: true,
      refetch: vi.fn(),
    };
    mocks.useShadowMCPInventory.mockImplementation(
      (request: { cursor?: string }) => {
        if (request.cursor === "next-page") {
          return loadingNextPageResponse;
        }

        return firstPageResponse;
      },
    );

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("First MCP")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    expect(screen.getByText("First MCP")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Loading" })).toBeTruthy();
    expect(screen.queryByText("Access Rules could not be loaded")).toBeFalsy();
  });

  it("keeps loaded inventory rows visible and retries after a next page error", async () => {
    const refetchNextPage = vi.fn();
    const firstPageServer = inventoryServer({
      canonicalServerUrl: "https://first.example.com/mcp",
      serverName: "First MCP",
    });
    const firstPageResponse = {
      data: { servers: [firstPageServer], nextCursor: "next-page" },
      error: null,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    };
    const errorNextPageResponse = {
      data: undefined,
      error: new Error("next page failed"),
      isFetching: false,
      isLoading: false,
      refetch: refetchNextPage,
    };
    mocks.useShadowMCPInventory.mockImplementation(
      (request: { cursor?: string }) => {
        if (request.cursor === "next-page") {
          return errorNextPageResponse;
        }

        return firstPageResponse;
      },
    );

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("First MCP")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    expect(screen.getByText("First MCP")).toBeTruthy();
    expect(screen.queryByText("Access Rules could not be loaded")).toBeFalsy();

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    expect(refetchNextPage).toHaveBeenCalled();
  });

  it("does not render stale inventory rows when the project changes", async () => {
    const firstProjectServer = inventoryServer({
      canonicalServerUrl: "https://first-project.example.com/mcp",
      serverName: "First Project MCP",
    });
    const firstProjectResponse = {
      data: { servers: [firstProjectServer], nextCursor: undefined },
      error: null,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    };
    const secondProjectLoadingResponse = {
      data: undefined,
      error: null,
      isFetching: true,
      isLoading: true,
      refetch: vi.fn(),
    };
    mocks.useShadowMCPInventory.mockImplementation(
      (request: { projectId: string }) => {
        if (request.projectId === "project-id-2") {
          return secondProjectLoadingResponse;
        }

        return firstProjectResponse;
      },
    );
    const queryClient = new QueryClient();
    const { rerender } = render(
      <QueryClientProvider client={queryClient}>
        <ShadowMCPInventoryTable
          policyState="blocking"
          projectID="project-id-1"
          projectSlug="project-slug-1"
        />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("First Project MCP")).toBeTruthy();
    });

    rerender(
      <QueryClientProvider client={queryClient}>
        <ShadowMCPInventoryTable
          policyState="blocking"
          projectID="project-id-2"
          projectSlug="project-slug-2"
        />
      </QueryClientProvider>,
    );

    expect(screen.queryByText("First Project MCP")).toBeFalsy();
  });

  it("does not render stale inventory rows when the table is disabled", async () => {
    const inventoryServerRow = inventoryServer({
      canonicalServerUrl: "https://enabled.example.com/mcp",
      serverName: "Enabled MCP",
    });
    const enabledResponse = {
      data: { servers: [inventoryServerRow], nextCursor: undefined },
      error: null,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    };
    mocks.useShadowMCPInventory.mockReturnValue(enabledResponse);
    const queryClient = new QueryClient();
    const { rerender } = render(
      <QueryClientProvider client={queryClient}>
        <ShadowMCPInventoryTable
          enabled
          policyState="blocking"
          projectID="project-id-1"
          projectSlug="project-slug-1"
        />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("Enabled MCP")).toBeTruthy();
    });

    rerender(
      <QueryClientProvider client={queryClient}>
        <ShadowMCPInventoryTable
          enabled={false}
          policyState="blocking"
          projectID="project-id-1"
          projectSlug="project-slug-1"
        />
      </QueryClientProvider>,
    );

    expect(screen.queryByText("Enabled MCP")).toBeFalsy();
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

    fireEvent.click(within(pendingRow).getByRole("button", { name: "Allow" }));

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
    });
    await waitFor(() => {
      expect(
        (
          within(pendingRow).getByRole("button", {
            name: "Allow",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false);
    });

    fireEvent.click(within(allowedRow).getByRole("button", { name: "Clear" }));

    await waitFor(() => {
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

  it("clears denied Shadow MCP inventory URL rules", async () => {
    const allowServer = vi.fn().mockResolvedValue({});
    const clearServer = vi.fn().mockResolvedValue({});
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "denied",
          canonicalServerUrl: "https://denied.example.com/mcp",
          serverName: "Denied MCP",
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
      expect(screen.getByText("Denied MCP")).toBeTruthy();
    });

    expect(screen.getByText("Blocked by URL rule")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Clear" }));

    await waitFor(() => {
      expect(clearServer).toHaveBeenCalledWith({
        request: {
          clearShadowMCPInventoryServerAccessRequestBody: {
            projectId: "project-id-1",
            serverUrl: "https://denied.example.com/mcp",
          },
        },
      });
    });
    expect(allowServer).not.toHaveBeenCalled();
  });

  it("uses the action button as the tooltip trigger", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://pending.example.com/mcp",
          serverName: "Pending MCP",
        }),
      ],
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("Pending MCP")).toBeTruthy();
    });

    expect(
      screen
        .getByRole("button", { name: "Allow" })
        .getAttribute("data-tooltip-trigger"),
    ).toBe("true");
  });

  it("shows a loading indicator on the clicked action", async () => {
    let resolveAllow: (value: unknown) => void = () => {};
    const allowPromise = new Promise((resolve) => {
      resolveAllow = resolve;
    });
    const allowServer = vi.fn().mockReturnValue(allowPromise);
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://pending.example.com/mcp",
          serverName: "Pending MCP",
        }),
      ],
    });
    mocks.allowInventoryServerMutation.mockReturnValue({
      isPending: false,
      mutateAsync: allowServer,
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("Pending MCP")).toBeTruthy();
    });

    const pendingRow = screen.getByText("Pending MCP").closest("tr");
    if (!pendingRow) {
      throw new Error("Inventory row not found");
    }

    fireEvent.click(within(pendingRow).getByRole("button", { name: "Allow" }));

    await waitFor(() => {
      expect(
        (
          within(pendingRow).getByRole("button", {
            name: "Allow",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(true);
      expect(pendingRow.querySelector(".animate-spin")).toBeTruthy();
    });

    resolveAllow({});

    await waitFor(() => {
      expect(pendingRow.querySelector(".animate-spin")).toBeFalsy();
    });
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

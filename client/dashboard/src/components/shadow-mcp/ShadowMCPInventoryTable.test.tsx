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
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { ShadowMCPInventoryTable } from "./ShadowMCPInventoryTable";

const mocks = vi.hoisted(() => ({
  useShadowMCPInventory: vi.fn(),
  invalidateShadowMCPInventory: vi.fn(),
  upsertPolicyBypassMutation: vi.fn(),
  deletePolicyBypassMutation: vi.fn(),
  resolveInventoryRequestMutation: vi.fn(),
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: mocks.invalidateShadowMCPInventory,
  useShadowMCPInventory: mocks.useShadowMCPInventory,
}));

vi.mock(
  "@gram/client/react-query/upsertShadowMCPInventoryPolicyBypass.js",
  () => ({
    useUpsertShadowMCPInventoryPolicyBypassMutation:
      mocks.upsertPolicyBypassMutation,
  }),
);

vi.mock(
  "@gram/client/react-query/deleteShadowMCPInventoryPolicyBypass.js",
  () => ({
    useDeleteShadowMCPInventoryPolicyBypassMutation:
      mocks.deletePolicyBypassMutation,
  }),
);

vi.mock("@gram/client/react-query/resolveShadowMCPInventoryRequest.js", () => ({
  useResolveShadowMCPInventoryRequestMutation:
    mocks.resolveInventoryRequestMutation,
}));

vi.mock("@speakeasy-api/moonshine", () => ({
  Badge: Object.assign(
    ({ children }: { children: ReactNode }) => <span>{children}</span>,
    {
      LeftIcon: ({ children }: { children: ReactNode }) => (
        <span>{children}</span>
      ),
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
      <button
        disabled={disabled}
        onClick={() => onClick?.()}
        type="button"
        {...props}
      >
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
  DropdownMenu: ({
    children,
    modal,
  }: {
    children: ReactNode;
    modal?: boolean;
  }) => <div data-dropdown-modal={String(modal)}>{children}</div>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    disabled,
    onClick,
    onSelect,
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    onSelect?: () => void;
  }) => (
    <button
      disabled={disabled}
      onClick={() => {
        onSelect?.();
        onClick?.();
      }}
    >
      {children}
    </button>
  ),
  DropdownMenuTrigger: ({
    asChild,
    children,
    ...props
  }: {
    asChild?: boolean;
    children: ReactNode;
    [key: string]: unknown;
  }) => {
    if (asChild && isValidElement(children)) {
      return cloneElement(
        children as ReactElement<Record<string, unknown>>,
        props,
      );
    }

    return <>{children}</>;
  },
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

vi.mock("@/components/ui/checkbox", () => ({
  Checkbox: ({
    checked,
    disabled,
    onCheckedChange,
  }: {
    checked?: boolean;
    disabled?: boolean;
    onCheckedChange?: (checked: boolean) => void;
  }) => (
    <input
      checked={checked}
      disabled={disabled}
      onChange={(event) => onCheckedChange?.(event.currentTarget.checked)}
      type="checkbox"
    />
  ),
}));

vi.mock("@/components/ui/radio-group", () => ({
  RadioGroup: ({
    children,
    onValueChange,
  }: {
    children: ReactNode;
    onValueChange?: (value: string) => void;
  }) => (
    <div
      onChange={(event) => {
        const target = event.target as HTMLInputElement;
        onValueChange?.(target.value);
      }}
    >
      {children}
    </div>
  ),
  RadioGroupItem: ({ value }: { value: string }) => (
    <input name="review-action" type="radio" value={value} />
  ),
}));

vi.mock("@/components/ui/sheet", () => ({
  Sheet: ({
    children,
    onOpenChange,
    open,
  }: {
    children: ReactNode;
    open?: boolean;
    onOpenChange?: (open: boolean) => void;
  }) =>
    open ? (
      <div data-testid="shadow-mcp-action-sheet">
        <button onClick={() => onOpenChange?.(false)}>Close panel</button>
        {children}
      </div>
    ) : null,
  SheetContent: ({ children }: { children: ReactNode }) => (
    <section>{children}</section>
  ),
  SheetDescription: ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  ),
  SheetFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
}));

function inventoryServer(
  overrides: Partial<ShadowMCPInventoryServer> & {
    canonicalServerUrl: string;
  },
): ShadowMCPInventoryServer {
  const { canonicalServerUrl, ...rest } = overrides;

  return {
    access: "none",
    allowedPolicyIds: [],
    canonicalServerUrl,
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastCalled: undefined,
    lastSeen: new Date("2026-01-02T10:00:00Z"),
    observedUseCount: 0,
    requestCount: 0,
    serverName: undefined,
    serverSlug: "example-com-mcp",
    topUsers: [],
    urlHost: new URL(canonicalServerUrl).host,
    userCount: 0,
    ...rest,
  };
}

type BlockingPolicy = Pick<
  RiskPolicy,
  "audienceType" | "audiencePrincipalUrns" | "id" | "name"
>;

function blockingPolicy(
  overrides: Partial<BlockingPolicy> = {},
): BlockingPolicy {
  return {
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    id: "policy-1",
    name: "Shadow MCP blocking policy",
    ...overrides,
  };
}

function role(overrides: Partial<Role> = {}): Role {
  return {
    createdAt: new Date("2026-01-01T00:00:00Z"),
    description: "Admin role",
    grants: [],
    id: "019f1e9c-09f8-7084-8011-678312db54fe",
    isSystem: false,
    memberCount: 1,
    name: "Admin",
    principalUrn: "role:organization:019f1e9c-09f8-7084-8011-678312db54fe",
    slug: "admin",
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    ...overrides,
  };
}

function member(overrides: Partial<AccessMember> = {}): AccessMember {
  return {
    email: "admin@example.com",
    id: "user-1",
    joinedAt: new Date("2026-01-01T00:00:00Z"),
    name: "Admin User",
    principalUrn: "user:user-1",
    roleIds: [],
    ...overrides,
  };
}

function renderInventoryTable(
  projectID = "project-id-1",
  policyState: "blocking" | "flagging" | "none" | "unavailable" = "blocking",
  shadowMCPPolicies = [blockingPolicy()],
  roles: Role[] = [],
  members: AccessMember[] = [],
) {
  const queryClient = new QueryClient();

  return render(
    <QueryClientProvider client={queryClient}>
      <ShadowMCPInventoryTable
        members={members}
        policyState={policyState}
        projectID={projectID}
        roles={roles}
        shadowMCPPolicies={shadowMCPPolicies}
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
    mocks.upsertPolicyBypassMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn(),
    });
    mocks.deletePolicyBypassMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn(),
    });
    mocks.resolveInventoryRequestMutation.mockReturnValue({
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

  it("shows request counts for each Shadow MCP server URL", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          canonicalServerUrl: "https://github.example.com/mcp",
          requestCount: 2,
          serverName: "GitHub MCP",
        }),
        inventoryServer({
          canonicalServerUrl: "https://slack.example.com/mcp",
          requestCount: 0,
          serverName: "Slack MCP",
        }),
      ],
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("GitHub MCP")).toBeTruthy();
    });

    expect(screen.getByText("2 Access Requests")).toBeTruthy();
    expect(screen.queryByText("0 Access Requests")).toBeFalsy();
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
    expect(
      screen.queryByText("Shadow MCP inventory could not be loaded"),
    ).toBeFalsy();
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
    expect(
      screen.queryByText("Shadow MCP inventory could not be loaded"),
    ).toBeFalsy();

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
          members={[]}
          policyState="blocking"
          projectID="project-id-1"
          roles={[]}
          shadowMCPPolicies={[blockingPolicy()]}
        />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("First Project MCP")).toBeTruthy();
    });

    rerender(
      <QueryClientProvider client={queryClient}>
        <ShadowMCPInventoryTable
          members={[]}
          policyState="blocking"
          projectID="project-id-2"
          roles={[]}
          shadowMCPPolicies={[blockingPolicy()]}
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
          members={[]}
          policyState="blocking"
          projectID="project-id-1"
          roles={[]}
          shadowMCPPolicies={[blockingPolicy()]}
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
          members={[]}
          policyState="blocking"
          projectID="project-id-1"
          roles={[]}
          shadowMCPPolicies={[blockingPolicy()]}
        />
      </QueryClientProvider>,
    );

    expect(screen.queryByText("Enabled MCP")).toBeFalsy();
  });

  it("adds and removes Shadow MCP inventory URL allow rules", async () => {
    const upsertPolicyBypass = vi.fn().mockResolvedValue({});
    const deletePolicyBypass = vi.fn().mockResolvedValue({});
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
    mocks.upsertPolicyBypassMutation.mockReturnValue({
      isPending: false,
      mutateAsync: upsertPolicyBypass,
    });
    mocks.deletePolicyBypassMutation.mockReturnValue({
      isPending: false,
      mutateAsync: deletePolicyBypass,
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
    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: "Add Allow Rule" }),
      ).toBeTruthy();
    });
    await waitFor(() => {
      expect(
        (
          within(screen.getByTestId("shadow-mcp-action-sheet")).getByRole(
            "button",
            { name: "Add Allow Rule" },
          ) as HTMLButtonElement
        ).disabled,
      ).toBe(false);
    });
    fireEvent.click(
      within(screen.getByTestId("shadow-mcp-action-sheet")).getByRole(
        "button",
        { name: "Add Allow Rule" },
      ),
    );

    await waitFor(() => {
      expect(upsertPolicyBypass).toHaveBeenCalledWith({
        request: {
          shadowMCPInventoryPolicyBypassForm: {
            policyIds: ["policy-1"],
            projectId: "project-id-1",
            serverUrl: "https://pending.example.com/mcp",
          },
        },
      });
    });

    fireEvent.click(
      within(allowedRow).getByRole("button", { name: "Delete Rule" }),
    );
    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Delete Rule" })).toBeTruthy();
    });
    fireEvent.click(
      within(screen.getByTestId("shadow-mcp-action-sheet")).getByRole(
        "button",
        { name: "Delete Rule" },
      ),
    );

    await waitFor(() => {
      expect(deletePolicyBypass).toHaveBeenCalledWith({
        request: {
          projectId: "project-id-1",
          serverUrl: "https://allowed.example.com/mcp",
        },
      });
    });
    expect(mocks.invalidateShadowMCPInventory).toHaveBeenCalled();
  });

  it("shows policy audience role names in the action sheet", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://pending.example.com/mcp",
          serverName: "Pending MCP",
        }),
      ],
    });

    renderInventoryTable(
      "project-id-1",
      "blocking",
      [
        blockingPolicy({
          audiencePrincipalUrns: [
            "role:organization:019f1e9c-09f8-7084-8011-678312db54fe",
          ],
          audienceType: "targeted",
          name: "Shadow MCP Scanner",
        }),
      ],
      [role()],
    );

    await waitFor(() => {
      expect(screen.getByText("Pending MCP")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Add Allow Rule" }));

    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: "Add Allow Rule" }),
      ).toBeTruthy();
    });
    expect(screen.getByText("Shadow MCP Scanner")).toBeTruthy();
    expect(screen.getByText("Policy applies to Admin")).toBeTruthy();
    expect(screen.queryByText("Policy applies to 1 selected")).toBeNull();
  });

  it("shows policy audience member names in the action sheet", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://pending.example.com/mcp",
          serverName: "Pending MCP",
        }),
      ],
    });

    renderInventoryTable(
      "project-id-1",
      "blocking",
      [
        blockingPolicy({
          audiencePrincipalUrns: ["user:user-1"],
          audienceType: "targeted",
          name: "Shadow MCP Scanner",
        }),
      ],
      [],
      [member()],
    );

    await waitFor(() => {
      expect(screen.getByText("Pending MCP")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Add Allow Rule" }));

    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: "Add Allow Rule" }),
      ).toBeTruthy();
    });
    expect(
      screen.getByText("Policy applies to Admin User (admin@example.com)"),
    ).toBeTruthy();
  });

  it("uses a non-modal action menu button", async () => {
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
        .getByRole("button", { name: "Open actions for Pending MCP" })
        .closest("[data-dropdown-modal]")
        ?.getAttribute("data-dropdown-modal"),
    ).toBe("false");
  });

  it("shows a loading indicator in the action sheet", async () => {
    let resolveAllowDecision: (value: unknown) => void = () => {};
    const allowDecisionPromise = new Promise((resolve) => {
      resolveAllowDecision = resolve;
    });
    const allowServer = vi.fn().mockReturnValue(allowDecisionPromise);
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://pending.example.com/mcp",
          serverName: "Pending MCP",
        }),
      ],
    });
    mocks.upsertPolicyBypassMutation.mockReturnValue({
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

    fireEvent.click(
      within(pendingRow).getByRole("button", { name: "Add Allow Rule" }),
    );
    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: "Add Allow Rule" }),
      ).toBeTruthy();
    });
    const actionSheet = screen.getByTestId("shadow-mcp-action-sheet");
    const submitButton = within(actionSheet).getByRole("button", {
      name: "Add Allow Rule",
    });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(allowServer).toHaveBeenCalled();
    });

    resolveAllowDecision({});

    await waitFor(() => {
      expect(
        screen.queryByRole("heading", { name: "Add Allow Rule" }),
      ).toBeNull();
      expect(
        (
          screen.getByRole("button", {
            name: "Open actions for Pending MCP",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false);
    });
  });

  it("resolves pending Shadow MCP requests by URL", async () => {
    const resolveInventoryRequest = vi.fn().mockResolvedValue({});
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "blocked",
          canonicalServerUrl: "https://requested.example.com/mcp",
          latestRequest: {
            id: "request-1",
            policyId: "policy-1",
            requestedAt: new Date("2026-01-04T11:30:00Z"),
            requesterEmail: "alex@example.com",
            requesterUserId: "user-1",
          },
          requestCount: 3,
          serverName: "Requested MCP",
        }),
      ],
    });
    mocks.resolveInventoryRequestMutation.mockReturnValue({
      isPending: false,
      mutateAsync: resolveInventoryRequest,
    });

    renderInventoryTable();

    await waitFor(() => {
      expect(screen.getByText("Requested MCP")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Review Request" }));
    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: "Review Request" }),
      ).toBeTruthy();
    });
    fireEvent.click(
      within(screen.getByTestId("shadow-mcp-action-sheet")).getByRole(
        "button",
        { name: "Approve Request" },
      ),
    );

    await waitFor(() => {
      expect(resolveInventoryRequest).toHaveBeenCalledWith({
        request: {
          resolveShadowMCPInventoryRequestForm: {
            decision: "allow",
            policyIds: ["policy-1"],
            projectId: "project-id-1",
            serverUrl: "https://requested.example.com/mcp",
          },
        },
      });
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

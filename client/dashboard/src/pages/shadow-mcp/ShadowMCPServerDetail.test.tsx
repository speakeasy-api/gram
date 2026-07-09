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
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import ShadowMCPServerDetail from "./ShadowMCPServerDetail";

const mocks = vi.hoisted(() => ({
  useDeleteShadowMCPInventoryPolicyBypassMutation: vi.fn(),
  useMembers: vi.fn(),
  useProject: vi.fn(),
  useResolveShadowMCPInventoryRequestMutation: vi.fn(),
  useRiskListPolicies: vi.fn(),
  useRoles: vi.fn(),
  useShadowMCPInventoryServer: vi.fn(),
  useShadowMCPInventoryUsers: vi.fn(),
  useUpsertShadowMCPInventoryPolicyBypassMutation: vi.fn(),
  invalidateShadowMCPInventory: vi.fn(),
  invalidateShadowMCPInventoryServer: vi.fn(),
  invalidateShadowMCPInventoryUsers: vi.fn(),
}));

vi.mock("react-router", () => ({
  useParams: () => ({
    serverUrl: encodeURIComponent("https://github.example.com/mcp"),
  }),
}));

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }

  function Header({ children }: { children?: ReactNode }) {
    return <div>{children}</div>;
  }
  Header.Breadcrumbs = () => null;

  function Body({ children }: { children: ReactNode }) {
    return <main>{children}</main>;
  }

  function Section({ children }: { children: ReactNode }) {
    return <section>{children}</section>;
  }
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h1>{children}</h1>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.CTA = ({ children }: { children: ReactNode }) => <>{children}</>;

  return {
    Page: Object.assign(Page, {
      Header,
      Body,
      Section,
    }),
  };
});

vi.mock("@/contexts/Auth", () => ({
  useProject: mocks.useProject,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: mocks.useRiskListPolicies,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.useMembers,
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: mocks.useRoles,
}));

vi.mock("@gram/client/react-query/shadowMCPInventoryServer.js", () => ({
  invalidateAllShadowMCPInventoryServer:
    mocks.invalidateShadowMCPInventoryServer,
  useShadowMCPInventoryServer: mocks.useShadowMCPInventoryServer,
}));

vi.mock("@gram/client/react-query/shadowMCPInventoryUsers.js", () => ({
  invalidateAllShadowMCPInventoryUsers: mocks.invalidateShadowMCPInventoryUsers,
  useShadowMCPInventoryUsers: mocks.useShadowMCPInventoryUsers,
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: mocks.invalidateShadowMCPInventory,
}));

vi.mock(
  "@gram/client/react-query/upsertShadowMCPInventoryPolicyBypass.js",
  () => ({
    useUpsertShadowMCPInventoryPolicyBypassMutation:
      mocks.useUpsertShadowMCPInventoryPolicyBypassMutation,
  }),
);

vi.mock(
  "@gram/client/react-query/deleteShadowMCPInventoryPolicyBypass.js",
  () => ({
    useDeleteShadowMCPInventoryPolicyBypassMutation:
      mocks.useDeleteShadowMCPInventoryPolicyBypassMutation,
  }),
);

vi.mock("@gram/client/react-query/resolveShadowMCPInventoryRequest.js", () => ({
  useResolveShadowMCPInventoryRequestMutation:
    mocks.useResolveShadowMCPInventoryRequestMutation,
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
  DropdownMenu: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    onSelect,
  }: {
    children: ReactNode;
    onSelect?: () => void;
  }) => <button onClick={() => onSelect?.()}>{children}</button>,
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
      }: {
        columns: Array<{ header: ReactNode; key: string }>;
      }) => (
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.key}>{column.header}</th>
            ))}
          </tr>
        </thead>
      ),
      Body: ({
        columns,
        data,
        handleLoadMore,
        hasMore,
        rowKey,
      }: {
        columns: Array<{
          key: string;
          render?: (row: { userKey: string }) => ReactNode;
        }>;
        data: Array<{ userKey: string }>;
        handleLoadMore?: () => void;
        hasMore?: boolean;
        rowKey: (row: { userKey: string }) => string;
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
                <button onClick={handleLoadMore}>Load more</button>
              </td>
            </tr>
          ) : null}
        </tbody>
      ),
    },
  ),
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

vi.mock("@/components/ui/skeleton", () => ({
  SkeletonTable: () => <div>Loading table</div>,
}));

function inventoryServer(
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    access: "allowed",
    allowedPolicyIds: ["policy-1"],
    canonicalServerUrl: "https://github.example.com/mcp",
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastCalled: new Date("2026-01-04T10:00:00Z"),
    lastSeen: new Date("2026-01-05T10:00:00Z"),
    observedUseCount: 8,
    requestCount: 0,
    serverName: "GitHub MCP",
    topUsers: ["alex@example.com"],
    urlHost: "github.example.com",
    userCount: 2,
    ...overrides,
  };
}

function renderDetailPage() {
  const queryClient = new QueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <ShadowMCPServerDetail />
    </QueryClientProvider>,
  );
}

describe("ShadowMCPServerDetail", () => {
  beforeEach(() => {
    for (const mock of Object.values(mocks)) {
      mock.mockReset();
    }

    mocks.useProject.mockReturnValue({
      id: "project-id-1",
      name: "Demo",
      slug: "demo",
    });
    mocks.useRiskListPolicies.mockReturnValue({
      data: {
        policies: [
          {
            action: "block",
            audiencePrincipalUrns: ["user:all"],
            audienceType: "everyone",
            enabled: true,
            id: "policy-1",
            name: "Shadow MCP blocking policy",
            sources: ["shadow_mcp"],
          },
        ],
      },
      isError: false,
      isLoading: false,
    });
    mocks.useMembers.mockReturnValue({
      data: { members: [] },
      isError: false,
      isLoading: false,
    });
    mocks.useRoles.mockReturnValue({
      data: { roles: [] },
      isError: false,
      isLoading: false,
    });
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer(),
      error: null,
      isLoading: false,
    });
    mocks.useShadowMCPInventoryUsers.mockReturnValue({
      data: {
        nextCursor: undefined,
        users: [
          {
            lastCalled: new Date("2026-01-04T10:00:00Z"),
            observedUseCount: 5,
            userKey: "alex@example.com",
          },
          {
            lastCalled: new Date("2026-01-03T10:00:00Z"),
            observedUseCount: 3,
            userKey: "sam@example.com",
          },
        ],
      },
      error: null,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    });
    mocks.useUpsertShadowMCPInventoryPolicyBypassMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn().mockResolvedValue({}),
    });
    mocks.useDeleteShadowMCPInventoryPolicyBypassMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn().mockResolvedValue({}),
    });
    mocks.useResolveShadowMCPInventoryRequestMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn().mockResolvedValue({}),
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("renders URL stats and top users for a Shadow MCP server", async () => {
    renderDetailPage();

    expect(screen.getByRole("heading", { name: "GitHub MCP" })).toBeTruthy();
    expect(screen.getAllByText("https://github.example.com/mcp")).toHaveLength(
      2,
    );
    expect(screen.getByText("Allowed")).toBeTruthy();
    expect(screen.getByText("8 calls")).toBeTruthy();
    expect(screen.getByText("2 users")).toBeTruthy();
    expect(screen.getByText("github.example.com")).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "User" })).toBeTruthy();
    expect(screen.getByText("alex@example.com")).toBeTruthy();
    expect(screen.getByText("5 calls")).toBeTruthy();
    expect(screen.getByText("sam@example.com")).toBeTruthy();

    expect(mocks.useShadowMCPInventoryServer).toHaveBeenCalledWith(
      {
        projectId: "project-id-1",
        serverUrl: "https://github.example.com/mcp",
      },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
    expect(mocks.useShadowMCPInventoryUsers).toHaveBeenCalledWith(
      {
        projectId: "project-id-1",
        serverUrl: "https://github.example.com/mcp",
        limit: 50,
      },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  it("adds an allow decision from the detail page action menu", async () => {
    const upsertPolicyBypass = vi.fn().mockResolvedValue({});
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({
        access: "none",
        allowedPolicyIds: [],
      }),
      error: null,
      isLoading: false,
    });
    mocks.useUpsertShadowMCPInventoryPolicyBypassMutation.mockReturnValue({
      isPending: false,
      mutateAsync: upsertPolicyBypass,
    });

    renderDetailPage();

    fireEvent.click(screen.getByRole("button", { name: "Add Allow Rule" }));
    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: "Add Allow Rule" }),
      ).toBeTruthy();
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
            serverUrl: "https://github.example.com/mcp",
          },
        },
      });
    });
    expect(mocks.invalidateShadowMCPInventory).toHaveBeenCalled();
  });
});

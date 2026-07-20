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
  useUpdateShadowMCPInventoryServerNameMutation: vi.fn(),
  useUpsertShadowMCPInventoryPolicyBypassMutation: vi.fn(),
  invalidateShadowMCPInventory: vi.fn(),
  invalidateShadowMCPInventoryServer: vi.fn(),
  invalidateShadowMCPInventoryUsers: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("react-router", () => ({
  useParams: () => ({
    serverSlug: "github-example-com-mcp-d8860eea",
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
  "@gram/client/react-query/updateShadowMCPInventoryServerName.js",
  () => ({
    useUpdateShadowMCPInventoryServerNameMutation:
      mocks.useUpdateShadowMCPInventoryServerNameMutation,
  }),
);

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

vi.mock("sonner", () => ({
  toast: {
    error: mocks.toastError,
    success: mocks.toastSuccess,
  },
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
    serverSlug: "github-example-com-mcp-d8860eea",
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
    mocks.useUpdateShadowMCPInventoryServerNameMutation.mockReturnValue({
      isPending: false,
      mutateAsync: vi.fn().mockResolvedValue({}),
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("renders summary stats and top users for a Shadow MCP server", async () => {
    renderDetailPage();

    expect(screen.getByRole("heading", { name: "GitHub MCP" })).toBeTruthy();
    expect(screen.getByText("https://github.example.com/mcp")).toBeTruthy();
    expect(screen.getByText("Allowed")).toBeTruthy();
    expect(screen.getByText("0 requests")).toBeTruthy();
    expect(screen.getByText("8 calls")).toBeTruthy();
    expect(screen.getByText("2 users")).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "User" })).toBeTruthy();
    expect(screen.getByText("alex@example.com")).toBeTruthy();
    expect(screen.getByText("5 calls")).toBeTruthy();
    expect(screen.getByText("sam@example.com")).toBeTruthy();

    expect(mocks.useShadowMCPInventoryServer).toHaveBeenCalledWith(
      {
        projectId: "project-id-1",
        serverSlug: "github-example-com-mcp-d8860eea",
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

  it("exposes the current server name and Policy-style editor attributes", () => {
    renderDetailPage();

    const renameButton = screen.getByRole("button", { name: "GitHub MCP" });
    expect(renameButton.getAttribute("title")).toBe("Rename Shadow MCP server");

    fireEvent.click(renameButton);

    const input = screen.getByRole("textbox", {
      name: "Shadow MCP server name",
    });
    expect(input).toBe(document.activeElement);
    expect(input.getAttribute("maxlength")).toBe("255");
  });

  it.each([
    ["loading", { data: undefined, error: null, isLoading: true }],
    [
      "unavailable",
      { data: undefined, error: new Error("load failed"), isLoading: false },
    ],
  ])(
    "does not expose the rename control while the server is %s",
    (_, query) => {
      mocks.useShadowMCPInventoryServer.mockReturnValue(query);

      renderDetailPage();

      expect(screen.queryByTitle("Rename Shadow MCP server")).toBeNull();
      expect(
        screen.queryByRole("textbox", { name: "Shadow MCP server name" }),
      ).toBeNull();
    },
  );

  it("edits the server name inline and saves once on Enter after both invalidations", async () => {
    const mutateAsync = vi.fn().mockResolvedValue({});
    let resolveServerInvalidation!: () => void;
    let resolveInventoryInvalidation!: () => void;
    mocks.invalidateShadowMCPInventoryServer.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveServerInvalidation = resolve;
      }),
    );
    mocks.invalidateShadowMCPInventory.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveInventoryInvalidation = resolve;
      }),
    );
    mocks.useUpdateShadowMCPInventoryServerNameMutation.mockReturnValue({
      isPending: false,
      mutateAsync,
    });

    renderDetailPage();
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", {
      name: "Shadow MCP server name",
    });
    fireEvent.change(input, { target: { value: "  Engineering GitHub  " } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({
        request: {
          updateShadowMCPInventoryServerNameForm: {
            projectId: "project-id-1",
            serverUrl: "https://github.example.com/mcp",
            name: "Engineering GitHub",
          },
        },
      });
    });
    expect(mutateAsync).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateShadowMCPInventoryServer).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateShadowMCPInventory).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole("textbox", { name: "Shadow MCP server name" }),
    ).toBeTruthy();

    resolveServerInvalidation();
    await Promise.resolve();
    expect(
      screen.getByRole("textbox", { name: "Shadow MCP server name" }),
    ).toBeTruthy();

    resolveInventoryInvalidation();
    await waitFor(() => {
      expect(
        screen.queryByRole("textbox", { name: "Shadow MCP server name" }),
      ).toBeNull();
    });
  });

  it("clears the custom server name with an empty string", async () => {
    const mutateAsync = vi.fn().mockResolvedValue({});
    mocks.useUpdateShadowMCPInventoryServerNameMutation.mockReturnValue({
      isPending: false,
      mutateAsync,
    });

    renderDetailPage();
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", {
      name: "Shadow MCP server name",
    });
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.blur(input);

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({
        request: {
          updateShadowMCPInventoryServerNameForm: {
            projectId: "project-id-1",
            serverUrl: "https://github.example.com/mcp",
            name: "",
          },
        },
      });
    });
  });

  it("keeps the editor open and reports an error when saving fails", async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error("save failed"));
    mocks.useUpdateShadowMCPInventoryServerNameMutation.mockReturnValue({
      isPending: false,
      mutateAsync,
    });

    renderDetailPage();
    fireEvent.click(screen.getByRole("button", { name: "GitHub MCP" }));
    const input = screen.getByRole("textbox", {
      name: "Shadow MCP server name",
    });
    fireEvent.change(input, { target: { value: "Engineering GitHub" } });
    fireEvent.blur(input);

    await waitFor(() => {
      expect(mocks.toastError).toHaveBeenCalledWith(
        "Unable to update Shadow MCP server name",
      );
    });
    expect(
      screen.getByRole("textbox", { name: "Shadow MCP server name" }),
    ).toBeTruthy();
  });

  it("uses the URL host when no custom server name exists", () => {
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({ serverName: undefined }),
      error: null,
      isLoading: false,
    });

    renderDetailPage();

    expect(
      screen.getByRole("button", { name: "github.example.com" }),
    ).toBeTruthy();
  });

  it("does not disable allow-rule actions while a rename is pending", () => {
    mocks.useUpdateShadowMCPInventoryServerNameMutation.mockReturnValue({
      isPending: true,
      mutateAsync: vi.fn().mockResolvedValue({}),
    });

    renderDetailPage();

    expect(
      screen
        .getByRole("button", { name: "Edit Rule" })
        .hasAttribute("disabled"),
    ).toBe(false);
    expect(
      screen
        .getByRole("button", { name: "Delete Rule" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });

  it("shows an empty state when the server has no user activity", () => {
    mocks.useShadowMCPInventoryUsers.mockReturnValue({
      data: {
        nextCursor: undefined,
        users: [],
      },
      error: null,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderDetailPage();

    expect(screen.getByText("No user activity")).toBeTruthy();
    expect(
      screen.getByText(
        "Users will appear here after this Shadow MCP server is called.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: "User" })).toBeNull();
  });

  it("shows review, edit, and delete actions for an allowed server with a pending request", () => {
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({ requestCount: 1 }),
      error: null,
      isLoading: false,
    });

    renderDetailPage();

    expect(screen.getByRole("button", { name: "Review Request" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Edit Rule" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete Rule" })).toBeTruthy();
  });

  it("keeps loaded users visible and retries after a next page error", async () => {
    const refetchNextPage = vi.fn();
    const firstPageResponse = {
      data: {
        nextCursor: "next-page",
        users: [
          {
            lastCalled: new Date("2026-01-04T10:00:00Z"),
            observedUseCount: 5,
            userKey: "alex@example.com",
          },
        ],
      },
      error: null,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    };
    const nextPageErrorResponse = {
      data: undefined,
      error: new Error("next page failed"),
      isFetching: false,
      isLoading: false,
      refetch: refetchNextPage,
    };
    mocks.useShadowMCPInventoryUsers.mockImplementation(
      (request: { cursor?: string }) => {
        if (request.cursor === "next-page") {
          return nextPageErrorResponse;
        }

        return firstPageResponse;
      },
    );

    renderDetailPage();

    await waitFor(() => {
      expect(screen.getByText("alex@example.com")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    expect(screen.getByText("alex@example.com")).toBeTruthy();
    expect(screen.queryByText("Users could not be loaded")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    expect(refetchNextPage).toHaveBeenCalled();
  });

  it("adds an allow decision from the detail page action menu", async () => {
    const upsertPolicyBypass = vi.fn().mockResolvedValue({});
    mocks.useRiskListPolicies.mockReturnValue({
      data: {
        policies: [
          {
            action: "block",
            audiencePrincipalUrns: ["user:all"],
            audienceType: "everyone",
            enabled: true,
            id: "policy-1",
            name: "Blocking policy",
            sources: ["shadow_mcp"],
          },
          {
            action: "flag",
            audiencePrincipalUrns: ["user:all"],
            audienceType: "everyone",
            enabled: true,
            id: "flag-policy",
            name: "Flag policy",
            sources: ["shadow_mcp"],
          },
          {
            action: "block",
            audiencePrincipalUrns: ["user:all"],
            audienceType: "everyone",
            enabled: false,
            id: "disabled-policy",
            name: "Disabled policy",
            sources: ["shadow_mcp"],
          },
          {
            action: "block",
            audiencePrincipalUrns: ["user:all"],
            audienceType: "everyone",
            enabled: true,
            id: "other-source-policy",
            name: "Other source policy",
            sources: ["prompt_injection"],
          },
        ],
      },
      isError: false,
      isLoading: false,
    });
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
    expect(screen.getByText("Blocking policy")).toBeTruthy();
    expect(screen.queryByText("Flag policy")).toBeNull();
    expect(screen.queryByText("Disabled policy")).toBeNull();
    expect(screen.queryByText("Other source policy")).toBeNull();
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

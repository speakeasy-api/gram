import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type ReactElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import ShadowMCPServerDetail from "./ShadowMCPServerDetail";

const mocks = vi.hoisted(() => ({
  useMembers: vi.fn(),
  useProject: vi.fn(),
  useRiskListPolicies: vi.fn(),
  useRoles: vi.fn(),
  useNavigate: vi.fn(),
  useRoutes: vi.fn(),
  useShadowMCPInventoryServer: vi.fn(),
  useShadowMCPInventoryUsers: vi.fn(),
  useUpdateShadowMCPInventoryServerNameMutation: vi.fn(),
  invalidateShadowMCPInventory: vi.fn(),
  ensureServerReview: vi.fn(),
  invalidateShadowMCPInventoryServer: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("react-router", () => ({
  useNavigate: mocks.useNavigate,
  useParams: () => ({
    serverSlug: "github-example-com-mcp-d8860eea",
  }),
}));

vi.mock("@/routes", () => ({
  useRoutes: mocks.useRoutes,
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

vi.mock("@/components/mcp-approvals/DecideAccessSheet", () => ({
  DecideAccessSheet: ({
    disposition,
    members,
    open,
    roles,
    target,
  }: {
    disposition: string | null;
    members: unknown[];
    open: boolean;
    roles: unknown[];
    target: {
      approvalRequestId?: string;
      canonicalServerUrl: string;
      displayName: string;
      pendingBypassRequestId?: string;
    } | null;
  }) =>
    open && target ? (
      <div
        data-testid="decide-access-sheet"
        data-approval-request-id={target.approvalRequestId}
        data-canonical-server-url={target.canonicalServerUrl}
        data-display-name={target.displayName}
        data-disposition={disposition ?? undefined}
        data-member-count={members.length}
        data-pending-bypass-request-id={target.pendingBypassRequestId}
        data-role-count={roles.length}
      />
    ) : null,
}));

vi.mock("@/components/mcp-approvals/ApprovalReview", () => ({
  // The double renders the usage and summary slots, because the real review
  // does: observed traffic and the at-a-glance strip are both sections of the
  // review, and a double that swallowed them would hide the page's own table
  // and stats from every test here.
  ApprovalReview: ({
    audience,
    requestId,
    usage,
    summary,
  }: {
    audience?: {
      disposition: string | null;
      members: unknown[];
      roles: unknown[];
    };
    requestId: string;
    usage?: React.ReactNode;
    summary?: React.ReactNode;
  }) => (
    <div
      data-testid="approval-review"
      data-audience-disposition={audience?.disposition ?? undefined}
      data-request-id={requestId}
    >
      {summary}
      {usage}
    </div>
  ),
  RefreshEvidenceButton: ({
    projectSlug,
    ready,
    requestId,
  }: {
    projectSlug: string;
    ready: boolean;
    requestId: string;
  }) => (
    <div
      data-testid="refresh-evidence-button"
      data-project-slug={projectSlug}
      data-ready={String(ready)}
      data-request-id={requestId}
    />
  ),
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
  useShadowMCPInventoryUsers: mocks.useShadowMCPInventoryUsers,
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: mocks.invalidateShadowMCPInventory,
}));

vi.mock("@gram/client/react-query/ensureMcpServerReview.js", () => ({
  useEnsureMcpServerReviewMutation: () => ({
    isPending: false,
    mutateAsync: mocks.ensureServerReview,
  }),
}));

vi.mock(
  "@gram/client/react-query/updateShadowMCPInventoryServerName.js",
  () => ({
    useUpdateShadowMCPInventoryServerNameMutation:
      mocks.useUpdateShadowMCPInventoryServerNameMutation,
  }),
);

vi.mock("@/components/ui/Badge", () => ({
  Badge: Object.assign(
    ({ children }: { children: ReactNode }) => <span>{children}</span>,
    {
      LeftIcon: ({ children }: { children: ReactNode }) => (
        <span>{children}</span>
      ),
      Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
    },
  ),
}));

vi.mock("@/components/ui/Button", () => ({
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
}));

vi.mock("@/components/ui/Table", () => ({
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
        isRowClickable,
        onRowClick,
        renderRow,
        rowKey,
      }: {
        columns: Array<{
          key: string;
          render?: (row: { userKey: string }) => ReactNode;
        }>;
        data: Array<{ userKey: string }>;
        handleLoadMore?: () => void;
        hasMore?: boolean;
        isRowClickable?: (row: { userKey: string }) => boolean;
        onRowClick?: (row: { userKey: string }) => void;
        renderRow?: (
          row: { userKey: string },
          rowElement: ReactElement,
        ) => ReactNode;
        rowKey: (row: { userKey: string }) => string;
      }) => (
        <tbody>
          {data.map((row) => {
            const rowElement = (
              <tr
                className={
                  isRowClickable?.(row) === false ? undefined : "cursor-pointer"
                }
                key={rowKey(row)}
                onClick={
                  isRowClickable?.(row) === false
                    ? undefined
                    : () => onRowClick?.(row)
                }
              >
                {columns.map((column) => (
                  <td key={column.key}>{column.render?.(row)}</td>
                ))}
              </tr>
            );

            return renderRow ? renderRow(row, rowElement) : rowElement;
          })}
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

vi.mock("@/components/ui/Skeleton", () => ({
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
    blockedPolicyIds: [],
    approvalRequest: {
      id: "request-default",
      requesterCount: 0,
      status: "unreviewed",
    },
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
    mocks.useNavigate.mockReturnValue(mocks.navigate);
    mocks.useRoutes.mockReturnValue({
      employees: {
        detail: { href: (userSlug: string) => `/employees/${userSlug}` },
      },
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
            email: "alex@example.com",
            lastCalled: new Date("2026-01-04T10:00:00Z"),
            observedUseCount: 15,
            sources: [
              { source: "claude-code", observedUseCount: 12 },
              { source: "cursor", observedUseCount: 3 },
              { source: "", observedUseCount: 1 },
            ],
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
    expect(screen.getByRole("columnheader", { name: "User" })).toBeTruthy();
    expect(screen.getByText("alex@example.com")).toBeTruthy();
    expect(screen.getByText("15 calls")).toBeTruthy();
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

  it("renders user sources and only links email-backed users to their employee page", () => {
    const lastCalled = new Date("2026-01-04T10:00:00Z");
    renderDetailPage();

    const emailRow = screen.getByText("alex@example.com").closest("tr");
    expect(emailRow).toBeTruthy();
    expect(within(emailRow!).getByText("Claude Code")).toBeTruthy();
    expect(within(emailRow!).getByText("Cursor")).toBeTruthy();
    expect(within(emailRow!).getByText("Unknown")).toBeTruthy();
    expect(within(emailRow!).getByText("12")).toBeTruthy();
    expect(within(emailRow!).getByText("3")).toBeTruthy();
    expect(within(emailRow!).getByText("1")).toBeTruthy();
    expect(within(emailRow!).getByText("15 calls")).toBeTruthy();
    expect(
      within(emailRow!).getByText(formatShortDate(lastCalled)),
    ).toBeTruthy();
    fireEvent.click(emailRow!);
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/employees/alex%40example.com",
    );

    const noEmailRow = screen.getByText("sam@example.com").closest("tr");
    expect(noEmailRow).toBeTruthy();
    expect(noEmailRow!.classList.contains("cursor-pointer")).toBe(false);
    fireEvent.click(noEmailRow!);
    expect(mocks.navigate).toHaveBeenCalledTimes(1);
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

  it("reviews a pending request through the decide access sheet", () => {
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({
        approvalRequest: {
          id: "request-1",
          requesterCount: 2,
          status: "requested",
        },
        requestCount: 1,
      }),
      error: null,
      isLoading: false,
    });

    renderDetailPage();

    expect(screen.queryByRole("button", { name: "Decide Access" })).toBeNull();
    expect(screen.queryByTestId("decide-access-sheet")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Review Request" }));

    const sheet = screen.getByTestId("decide-access-sheet");
    expect(sheet.getAttribute("data-canonical-server-url")).toBe(
      "https://github.example.com/mcp",
    );
    expect(sheet.getAttribute("data-display-name")).toBe("GitHub MCP");
    expect(sheet.getAttribute("data-approval-request-id")).toBe("request-1");
    expect(sheet.getAttribute("data-disposition")).toBe("block_all");
  });

  it("decides access proactively on a server with only a dossier", () => {
    renderDetailPage();

    expect(screen.queryByRole("button", { name: "Review Request" })).toBeNull();
    expect(screen.queryByTestId("decide-access-sheet")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Decide Access" }));

    const sheet = screen.getByTestId("decide-access-sheet");
    expect(sheet.getAttribute("data-canonical-server-url")).toBe(
      "https://github.example.com/mcp",
    );
    expect(sheet.getAttribute("data-display-name")).toBe("GitHub MCP");
    // The decision lands on the dossier the page already resolved.
    expect(sheet.getAttribute("data-approval-request-id")).toBe(
      "request-default",
    );
  });

  it("renders the access review only when an approval request exists", () => {
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({ approvalRequest: undefined }),
      error: null,
      isLoading: false,
    });
    renderDetailPage();

    expect(screen.getByText("Gathering evidence")).toBeTruthy();
    expect(mocks.ensureServerReview).toHaveBeenCalledWith({
      request: {
        gramProject: "demo",
        ensureServerReviewRequestBody: {
          target: "https://github.example.com/mcp",
        },
      },
    });
    expect(screen.queryByTestId("approval-review")).toBeNull();

    cleanup();
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({
        approvalRequest: {
          id: "request-2",
          requesterCount: 1,
          status: "approved",
        },
      }),
      error: null,
      isLoading: false,
    });
    renderDetailPage();

    expect(screen.queryByText("Gathering evidence")).toBeNull();
    const review = screen.getByTestId("approval-review");
    expect(review.getAttribute("data-request-id")).toBe("request-2");
  });

  it("carries the pending legacy bypass request into the decide sheet", () => {
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({
        approvalRequest: {
          id: "request-1",
          requesterCount: 1,
          status: "requested",
        },
        latestRequest: {
          id: "legacy-bypass-1",
          policyId: "policy-1",
          requestedAt: new Date("2026-01-03T10:00:00Z"),
          requesterEmail: "alex@example.com",
          requesterUserId: "user-1",
        },
        requestCount: 1,
      }),
      error: null,
      isLoading: false,
    });

    renderDetailPage();

    fireEvent.click(screen.getByRole("button", { name: "Review Request" }));

    const sheet = screen.getByTestId("decide-access-sheet");
    expect(sheet.getAttribute("data-pending-bypass-request-id")).toBe(
      "legacy-bypass-1",
    );
  });

  it("shows a failure panel when evidence gathering fails and retries on demand", async () => {
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({ approvalRequest: undefined }),
      error: null,
      isLoading: false,
    });
    mocks.ensureServerReview.mockRejectedValueOnce(new Error("gather failed"));
    mocks.ensureServerReview.mockResolvedValue({});

    renderDetailPage();

    await waitFor(() => {
      expect(screen.getByText("Evidence could not be gathered")).toBeTruthy();
    });
    expect(mocks.ensureServerReview).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => {
      expect(mocks.ensureServerReview).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByText("Evidence could not be gathered")).toBeNull();
    });
    expect(screen.getByText("Gathering evidence")).toBeTruthy();
  });

  it("does not let a stale failure mark a newer server's gather as failed", async () => {
    const rejecters: Array<(error: Error) => void> = [];
    mocks.ensureServerReview.mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          rejecters.push(reject);
        }),
    );
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({ approvalRequest: undefined }),
      error: null,
      isLoading: false,
    });

    const queryClient = new QueryClient();
    const view = render(
      <QueryClientProvider client={queryClient}>
        <ShadowMCPServerDetail />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(mocks.ensureServerReview).toHaveBeenCalledTimes(1);
    });

    // Navigating to another unreviewed server starts a new gather run.
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({
        approvalRequest: undefined,
        canonicalServerUrl: "https://other.example.com/mcp",
        serverName: "Other MCP",
        urlHost: "other.example.com",
      }),
      error: null,
      isLoading: false,
    });
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <ShadowMCPServerDetail />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(mocks.ensureServerReview).toHaveBeenCalledTimes(2);
    });

    // The first server's rejection lands late; it belongs to an older run
    // and must not fail the newer one.
    await act(async () => {
      rejecters[0]!(new Error("stale failure"));
      await Promise.resolve();
    });

    expect(screen.queryByText("Evidence could not be gathered")).toBeNull();
    expect(screen.getByText("Gathering evidence")).toBeTruthy();
  });

  it("renders the refresh evidence control only when a review exists", () => {
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({ approvalRequest: undefined }),
      error: null,
      isLoading: false,
    });
    renderDetailPage();

    expect(screen.queryByTestId("refresh-evidence-button")).toBeNull();

    cleanup();
    mocks.useShadowMCPInventoryServer.mockReturnValue({
      data: inventoryServer({
        approvalRequest: {
          id: "request-3",
          requesterCount: 1,
          status: "denied",
        },
      }),
      error: null,
      isLoading: false,
    });
    renderDetailPage();

    const refresh = screen.getByTestId("refresh-evidence-button");
    expect(refresh.getAttribute("data-request-id")).toBe("request-3");
    expect(refresh.getAttribute("data-project-slug")).toBe("demo");
    expect(refresh.getAttribute("data-ready")).toBe("true");
  });
});

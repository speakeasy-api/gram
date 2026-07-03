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
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { RiskPolicyBypassRequest } from "@gram/client/models/components/riskpolicybypassrequest.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

const mocks = vi.hoisted(() => ({
  useRBAC: vi.fn(),
  usePolicyBypassRequests: vi.fn(),
  useShadowMCPInventory: vi.fn(),
  invalidateShadowMCPInventory: vi.fn(),
  allowInventoryServerMutation: vi.fn(),
  blockInventoryServerMutation: vi.fn(),
  clearInventoryServerMutation: vi.fn(),
  useRoles: vi.fn(),
  useMembers: vi.fn(),
  mutation: vi.fn(),
  invalidate: vi.fn(),
  useQueryState: vi.fn(),
  setReviewParam: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: mocks.useRBAC,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
  }: {
    children: ReactNode | ((props: { disabled: boolean }) => ReactNode);
    scope: string | string[];
  }) => {
    const { hasAnyScope } = mocks.useRBAC();
    const scopes = Array.isArray(scope) ? scope : [scope];
    if (!hasAnyScope(scopes)) return null;

    return (
      <>
        {typeof children === "function"
          ? children({ disabled: false })
          : children}
      </>
    );
  },
}));

vi.mock("@gram/client/react-query/riskListPolicyBypassRequests.js", () => ({
  invalidateAllRiskListPolicyBypassRequests: mocks.invalidate,
  useRiskListPolicyBypassRequests: mocks.usePolicyBypassRequests,
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

vi.mock("@gram/client/react-query/riskApprovePolicyBypassRequest.js", () => ({
  useRiskApprovePolicyBypassRequestMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/riskDenyPolicyBypassRequest.js", () => ({
  useRiskDenyPolicyBypassRequestMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: mocks.useRoles,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.useMembers,
}));

vi.mock("nuqs", () => ({
  useQueryState: mocks.useQueryState,
}));

type TableRow = RiskPolicyBypassRequest | ShadowMCPInventoryServer;

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
      LeftIcon: ({ children }: { children: ReactNode }) => (
        <span>{children}</span>
      ),
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
      render?: (row: TableRow) => ReactNode;
    }>;
    data: TableRow[];
    rowKey: (row: TableRow) => string;
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

vi.mock("@/components/ui/radio-group", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  const RadioContext = React.createContext<
    | {
        value?: string;
        onValueChange?: (value: string) => void;
      }
    | undefined
  >(undefined);

  return {
    RadioGroup: ({
      children,
      value,
      onValueChange,
    }: {
      children: ReactNode;
      value?: string;
      onValueChange?: (value: string) => void;
    }) => (
      <RadioContext.Provider value={{ value, onValueChange }}>
        <div>{children}</div>
      </RadioContext.Provider>
    ),
    RadioGroupItem: ({ value }: { value: string }) => {
      const context = React.useContext(RadioContext);
      return (
        <input
          type="radio"
          checked={context?.value === value}
          onChange={() => context?.onValueChange?.(value)}
        />
      );
    },
  };
});

vi.mock("@/components/ui/select", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  const SelectContext = React.createContext<
    | {
        onValueChange?: (value: string) => void;
      }
    | undefined
  >(undefined);

  return {
    Select: ({
      children,
      onValueChange,
    }: {
      children: ReactNode;
      value?: string;
      onValueChange?: (value: string) => void;
      disabled?: boolean;
    }) => (
      <SelectContext.Provider value={{ onValueChange }}>
        <div>{children}</div>
      </SelectContext.Provider>
    ),
    SelectContent: ({ children }: { children: ReactNode }) => (
      <div>{children}</div>
    ),
    SelectItem: ({
      children,
      value,
    }: {
      children: ReactNode;
      value: string;
    }) => {
      const context = React.useContext(SelectContext);
      return (
        <button type="button" onClick={() => context?.onValueChange?.(value)}>
          {children}
        </button>
      );
    },
    SelectTrigger: ({ children }: { children: ReactNode }) => (
      <div>{children}</div>
    ),
    SelectValue: ({ placeholder }: { placeholder?: string }) => (
      <span>{placeholder}</span>
    ),
  };
});

vi.mock("@/components/ui/sheet", () => ({
  Sheet: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  SheetDescription: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  SheetFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetTitle: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { ApprovalRequestsContent } from "./ApprovalRequestsContent";

function policyBypassRequest({
  id,
  label,
  serverURL,
  requesterEmail = "requester@example.com",
  status = "requested",
  grantedPrincipalUrns = [],
}: {
  id: string;
  label: string;
  serverURL: string;
  requesterEmail?: string;
  status?: RiskPolicyBypassRequest["status"];
  grantedPrincipalUrns?: string[];
}): RiskPolicyBypassRequest {
  return {
    id,
    createdAt: new Date("2026-01-01"),
    updatedAt: new Date("2026-01-02"),
    grantedPrincipalUrns,
    policyId: "policy-1",
    requesterEmail,
    requesterUserId: "user-1",
    status,
    targetDimensions: { server_url: serverURL },
    targetKey: serverURL,
    targetKind: "shadow_mcp_server",
    targetLabel: label,
  };
}

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

function accessMember({
  id,
  email,
  name = email,
  roleIds = [],
}: {
  id: string;
  email: string;
  name?: string;
  roleIds?: string[];
}): AccessMember {
  return {
    email,
    id,
    joinedAt: new Date("2026-01-01"),
    name,
    principalUrn: `user:${id}`,
    roleIds,
  };
}

function accessRole({
  id,
  name,
  isSystem = false,
}: {
  id: string;
  name: string;
  isSystem?: boolean;
}): Role {
  return {
    createdAt: new Date("2026-01-01"),
    description: "",
    grants: [],
    id,
    isSystem,
    memberCount: 0,
    name,
    principalUrn: `role:organization:${id}`,
    slug: name.toLowerCase(),
    updatedAt: new Date("2026-01-01"),
  };
}

function mockPolicyBypassLists({
  requested = [],
}: {
  requested?: RiskPolicyBypassRequest[];
} = {}) {
  mocks.usePolicyBypassRequests.mockImplementation(
    ({ status }: { status?: RiskPolicyBypassRequest["status"] }) => ({
      data: {
        requests: status === "requested" ? requested : [],
      },
      isLoading: false,
      error: null,
    }),
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

function mockAdminRBAC() {
  mocks.useRBAC.mockReturnValue({
    hasScope: (scope: string) => scope === "org:admin",
    hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
    hasAllScopes: () => true,
    isLoading: false,
  });
}

function renderContent(
  projectSlug = "project-1",
  projectId = "project-id-1",
  path = "/speakeasy/projects/project-1/approval-requests",
) {
  const queryClient = new QueryClient();

  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/:orgSlug/projects/:projectSlug/approval-requests"
          element={
            <QueryClientProvider client={queryClient}>
              <ApprovalRequestsContent
                projectID={projectId}
                projectSlug={projectSlug}
              />
            </QueryClientProvider>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  for (const mock of Object.values(mocks)) {
    mock.mockReset();
  }

  mockPolicyBypassLists();
  mockShadowMCPInventory();
  mocks.mutation.mockReturnValue({
    isPending: false,
    mutateAsync: vi.fn(),
  });
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
  mocks.useQueryState.mockReturnValue([null, mocks.setReviewParam]);
  mocks.useRoles.mockReturnValue({
    data: {
      roles: [
        accessRole({ id: "role-admin", name: "Admin", isSystem: true }),
        accessRole({ id: "role-reviewers", name: "Reviewers" }),
      ],
    },
  });
  mocks.useMembers.mockReturnValue({
    data: {
      members: [
        accessMember({
          id: "user-1",
          email: "requester@example.com",
          name: "Requester",
          roleIds: ["role-reviewers"],
        }),
        accessMember({
          id: "user-2",
          email: "reviewer@example.com",
          name: "Reviewer",
        }),
      ],
    },
  });
});

afterEach(() => {
  cleanup();
});

describe("ApprovalRequestsContent", () => {
  it("renders empty states for approval requests and inventory access", async () => {
    mockAdminRBAC();

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("No approval requests")).toBeTruthy();
      expect(screen.getByText("No Shadow MCP servers")).toBeTruthy();
    });
    expect(
      screen.getByText(
        "Requests will appear here when users ask for access after a policy block.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Inventory URLs will appear here after hook startup captures configured Shadow MCP servers.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add Rule" })).toBeNull();
  });

  it("does not load or render approval requests for non-admin org readers", () => {
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:read",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:read"),
      hasAllScopes: () => false,
      isLoading: false,
    });

    renderContent();

    expect(mocks.usePolicyBypassRequests).toHaveBeenCalledWith(
      { status: "requested", gramProject: "project-1" },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(mocks.useShadowMCPInventory).toHaveBeenCalledWith(
      { projectId: "project-id-1", limit: 50 },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(
      screen.queryByRole("heading", { name: "Approval Requests" }),
    ).toBeNull();
    expect(screen.getByRole("heading", { name: "Access Rules" })).toBeTruthy();
  });

  it("does not load policy access data without a project id", () => {
    mockAdminRBAC();

    renderContent("", "");

    expect(mocks.usePolicyBypassRequests).toHaveBeenCalledWith(
      { status: "requested", gramProject: "" },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(mocks.useShadowMCPInventory).toHaveBeenCalledWith(
      { projectId: "", limit: 50 },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
  });

  it("renders requested policy bypass requests from the policy access workflow", async () => {
    mockPolicyBypassLists({
      requested: [
        policyBypassRequest({
          id: "request-1",
          label: "First request",
          serverURL: "https://first.example.com/mcp",
          requesterEmail: "first@example.com",
        }),
        policyBypassRequest({
          id: "request-2",
          label: "Second request",
          serverURL: "https://second.example.com/mcp",
          requesterEmail: "second@example.com",
        }),
      ],
    });
    mockAdminRBAC();

    renderContent();

    await waitFor(() => {
      expect(screen.getAllByText("First request").length).toBeGreaterThan(0);
    });
    const requestsSection = screen
      .getByRole("heading", { name: "Approval Requests" })
      .closest("section");
    if (!requestsSection) throw new Error("Requests section not found");

    expect(
      within(requestsSection).getByRole("columnheader", { name: "Type" }),
    ).toBeTruthy();
    expect(within(requestsSection).getAllByText("Shadow MCP")).toHaveLength(2);
    expect(within(requestsSection).getByText("Second request")).toBeTruthy();
    expect(mocks.usePolicyBypassRequests).toHaveBeenCalledWith(
      { status: "requested", gramProject: "project-1" },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  it("opens a requested policy bypass request from the review query param", async () => {
    mockPolicyBypassLists({
      requested: [
        policyBypassRequest({
          id: "request-linked",
          label: "Linked request",
          serverURL: "https://linked.example.com/mcp",
        }),
      ],
    });
    mocks.useQueryState.mockReturnValue([
      "request-linked",
      mocks.setReviewParam,
    ]);
    mockAdminRBAC();

    renderContent(
      "project-1",
      "project-id-1",
      "/speakeasy/projects/project-1/approval-requests?review=request-linked",
    );

    await waitFor(() => {
      expect(screen.getByText("Review request")).toBeTruthy();
    });
    expect(screen.getAllByText("Linked request").length).toBeGreaterThan(0);
  });

  it("denies requests through the policy access workflow", async () => {
    const denyRequest = vi.fn().mockResolvedValue({});
    mockPolicyBypassLists({
      requested: [
        policyBypassRequest({
          id: "request-deny",
          label: "Denied request",
          serverURL: "https://blocked.example.com/mcp",
        }),
      ],
    });
    mockAdminRBAC();
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: denyRequest,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Denied request")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(screen.getAllByRole("radio")[1]!);
    fireEvent.click(screen.getByRole("button", { name: "Deny request" }));

    await waitFor(() => {
      expect(denyRequest).toHaveBeenCalledWith({
        request: {
          gramProject: "project-1",
          riskIDRequestBody: { id: "request-deny" },
        },
      });
    });
  });

  it("approves requests through the policy access workflow", async () => {
    const approveRequest = vi.fn().mockResolvedValue({});
    mockPolicyBypassLists({
      requested: [
        policyBypassRequest({
          id: "request-approve",
          label: "Approve request",
          serverURL: "https://allowed.example.com/mcp",
        }),
      ],
    });
    mockAdminRBAC();
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: approveRequest,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Approve request")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(screen.getByRole("button", { name: "Approve request" }));

    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith({
        request: {
          gramProject: "project-1",
          riskPolicyBypassApprovalRequestBody: {
            id: "request-approve",
            grantedPrincipalUrns: ["user:user-1"],
          },
        },
      });
    });
  });

  it("approves requests for every organization user", async () => {
    const approveRequest = vi.fn().mockResolvedValue({});
    mockPolicyBypassLists({
      requested: [
        policyBypassRequest({
          id: "request-everyone",
          label: "Everyone request",
          serverURL: "https://everyone.example.com/mcp",
        }),
      ],
    });
    mockAdminRBAC();
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: approveRequest,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Everyone request")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(screen.getAllByRole("radio")[2]!);
    fireEvent.click(screen.getByRole("button", { name: "Approve request" }));

    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith({
        request: {
          gramProject: "project-1",
          riskPolicyBypassApprovalRequestBody: {
            id: "request-everyone",
            grantedPrincipalUrns: ["user:all"],
          },
        },
      });
    });
  });

  it("approves requests for a selected role", async () => {
    const approveRequest = vi.fn().mockResolvedValue({});
    mockPolicyBypassLists({
      requested: [
        policyBypassRequest({
          id: "request-role",
          label: "Role request",
          serverURL: "https://role.example.com/mcp",
        }),
      ],
    });
    mockAdminRBAC();
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: approveRequest,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Role request")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(screen.getAllByRole("radio")[3]!);
    expect(screen.getByText("Current")).toBeTruthy();
    const reviewersOption = screen.getByText("Reviewers").closest("button");
    if (!reviewersOption) throw new Error("Reviewers option not found");
    fireEvent.click(reviewersOption);
    fireEvent.click(screen.getByRole("button", { name: "Approve request" }));

    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith({
        request: {
          gramProject: "project-1",
          riskPolicyBypassApprovalRequestBody: {
            id: "request-role",
            grantedPrincipalUrns: ["role:organization:role-reviewers"],
          },
        },
      });
    });
  });

  it("renders Shadow MCP inventory rows as access rules", async () => {
    mockShadowMCPInventory({
      servers: [
        inventoryServer({
          access: "allowed",
          canonicalServerUrl: "https://github.example.com/mcp",
          lastCalled: new Date("2026-01-02T11:30:00Z"),
          observedUseCount: 42,
          rule: {
            accessScope: "project",
            displayName: "GitHub MCP",
            disposition: "allowed",
            id: "rule-1",
            matchBreadth: "full_url",
            matchValue: "https://github.example.com/mcp",
            projectId: "project-id-1",
          },
          serverName: "GitHub MCP",
          topUsers: ["ava@example.com", "bea@example.com"],
          userCount: 3,
        }),
        inventoryServer({
          access: "none",
          canonicalServerUrl: "https://unused.example.com/mcp",
          lastCalled: undefined,
          lastSeen: new Date("2026-01-03T11:30:00Z"),
        }),
      ],
    });
    mockAdminRBAC();

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("GitHub MCP")).toBeTruthy();
    });
    const accessRulesSection = screen
      .getByRole("heading", { name: "Access Rules" })
      .closest("section");
    if (!accessRulesSection) throw new Error("Access Rules section not found");

    expect(
      within(accessRulesSection).getByText("https://github.example.com/mcp"),
    ).toBeTruthy();
    expect(within(accessRulesSection).getByText("Allowed")).toBeTruthy();
    expect(within(accessRulesSection).getByText("42 calls")).toBeTruthy();
    expect(within(accessRulesSection).getByText("3 users")).toBeTruthy();
    expect(
      within(accessRulesSection).getByText("ava@example.com, bea@example.com"),
    ).toBeTruthy();
    expect(within(accessRulesSection).getByText("Never")).toBeTruthy();
    expect(
      within(accessRulesSection).getByText("https://unused.example.com/mcp"),
    ).toBeTruthy();
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
    mockAdminRBAC();
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

    renderContent();

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

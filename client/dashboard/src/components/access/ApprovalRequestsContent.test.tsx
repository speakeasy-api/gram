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

const mocks = vi.hoisted(() => ({
  useRBAC: vi.fn(),
  usePolicyBypassRequests: vi.fn(),
  useRoles: vi.fn(),
  useMembers: vi.fn(),
  mutation: vi.fn(),
  invalidate: vi.fn(),
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

vi.mock("@gram/client/react-query/riskApprovePolicyBypassRequest.js", () => ({
  useRiskApprovePolicyBypassRequestMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/riskDenyPolicyBypassRequest.js", () => ({
  useRiskDenyPolicyBypassRequestMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/riskRevokePolicyBypassRequest.js", () => ({
  useRiskRevokePolicyBypassRequestMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: mocks.useRoles,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.useMembers,
}));

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
  Dialog: Object.assign(
    ({
      children,
      open,
    }: {
      children: ReactNode;
      open?: boolean;
      onOpenChange?: (open: boolean) => void;
    }) => (open ? <div role="dialog">{children}</div> : null),
    {
      Content: ({ children }: { children: ReactNode }) => <div>{children}</div>,
      Header: ({ children }: { children: ReactNode }) => <div>{children}</div>,
      Title: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
      Footer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
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
      render?: (row: RiskPolicyBypassRequest) => ReactNode;
    }>;
    data: RiskPolicyBypassRequest[];
    rowKey: (row: RiskPolicyBypassRequest) => string;
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
    slug: name.toLowerCase(),
    updatedAt: new Date("2026-01-01"),
  };
}

function mockPolicyBypassLists({
  requested = [],
  approved = [],
}: {
  requested?: RiskPolicyBypassRequest[];
  approved?: RiskPolicyBypassRequest[];
} = {}) {
  mocks.usePolicyBypassRequests.mockImplementation(
    ({ status }: { status?: RiskPolicyBypassRequest["status"] }) => ({
      data: {
        requests: status === "approved" ? approved : requested,
      },
      isLoading: false,
      error: null,
    }),
  );
}

function mockAdminRBAC() {
  mocks.useRBAC.mockReturnValue({
    hasScope: (scope: string) => scope === "org:admin",
    hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
    hasAllScopes: () => true,
    isLoading: false,
  });
}

function renderContent(projectId = "project-1") {
  const queryClient = new QueryClient();

  return render(
    <MemoryRouter
      initialEntries={["/speakeasy/projects/project-1/approval-requests"]}
    >
      <Routes>
        <Route
          path="/:orgSlug/projects/:projectSlug/approval-requests"
          element={
            <QueryClientProvider client={queryClient}>
              <ApprovalRequestsContent projectId={projectId} />
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
  mocks.mutation.mockReturnValue({
    isPending: false,
    mutateAsync: vi.fn(),
  });
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
  it("renders empty states for approval requests and approved access", async () => {
    mockAdminRBAC();

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("No approval requests")).toBeTruthy();
      expect(screen.getByText("No access rules")).toBeTruthy();
    });
    expect(
      screen.getByText(
        "Requests will appear here when users ask for access after a policy block.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("Approved policy bypass requests will appear here."),
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
      { status: "requested" },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(mocks.usePolicyBypassRequests).toHaveBeenCalledWith(
      { status: "approved" },
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

    renderContent("");

    expect(mocks.usePolicyBypassRequests).toHaveBeenCalledWith(
      { status: "requested" },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(mocks.usePolicyBypassRequests).toHaveBeenCalledWith(
      { status: "approved" },
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
      { status: "requested" },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
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
    fireEvent.click(screen.getAllByRole("radio")[1]);
    fireEvent.click(screen.getByRole("button", { name: "Deny request" }));

    await waitFor(() => {
      expect(denyRequest).toHaveBeenCalledWith({
        request: {
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
    fireEvent.click(screen.getAllByRole("radio")[2]);
    fireEvent.click(screen.getByRole("button", { name: "Approve request" }));

    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith({
        request: {
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
    fireEvent.click(screen.getAllByRole("radio")[3]);
    expect(screen.getByText("Current")).toBeTruthy();
    const reviewersOption = screen.getByText("Reviewers").closest("button");
    if (!reviewersOption) throw new Error("Reviewers option not found");
    fireEvent.click(reviewersOption);
    fireEvent.click(screen.getByRole("button", { name: "Approve request" }));

    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith({
        request: {
          riskPolicyBypassApprovalRequestBody: {
            id: "request-role",
            grantedPrincipalUrns: ["role:organization:role-reviewers"],
          },
        },
      });
    });
  });

  it("renders approved policy bypass requests as access rules", async () => {
    mockPolicyBypassLists({
      approved: [
        policyBypassRequest({
          id: "request-approved",
          label: "Datadog MCP",
          serverURL: "https://datadog.example.com/mcp",
          status: "approved",
          grantedPrincipalUrns: [
            "user:all",
            "role:organization:role-reviewers",
          ],
        }),
      ],
    });
    mockAdminRBAC();

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Datadog MCP")).toBeTruthy();
    });
    const accessRulesSection = screen
      .getByRole("heading", { name: "Access Rules" })
      .closest("section");
    if (!accessRulesSection) throw new Error("Access Rules section not found");

    expect(within(accessRulesSection).getByText("Approved")).toBeTruthy();
    expect(
      within(accessRulesSection).getByRole("columnheader", {
        name: "Applies to",
      }),
    ).toBeTruthy();
    expect(
      within(accessRulesSection).getByText("Everyone, Reviewers"),
    ).toBeTruthy();
    expect(within(accessRulesSection).getByText("Shadow MCP")).toBeTruthy();
  });

  it("edits the principals for approved policy bypass access", async () => {
    const approveRequest = vi.fn().mockResolvedValue({});
    mockPolicyBypassLists({
      approved: [
        policyBypassRequest({
          id: "request-edit",
          label: "Editable access",
          serverURL: "https://edit.example.com/mcp",
          status: "approved",
          grantedPrincipalUrns: ["user:all"],
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
      expect(screen.getByText("Editable access")).toBeTruthy();
    });
    const row = screen.getByText("Editable access").closest("tr");
    if (!row) throw new Error("Approved request row not found");
    fireEvent.click(within(row).getByRole("button", { name: "Edit" }));

    expect(screen.getByText("Edit access")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("radio")[1]);
    const reviewersOption = screen.getByText("Reviewers").closest("button");
    if (!reviewersOption) throw new Error("Reviewers option not found");
    fireEvent.click(reviewersOption);
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith({
        request: {
          riskPolicyBypassApprovalRequestBody: {
            id: "request-edit",
            grantedPrincipalUrns: ["role:organization:role-reviewers"],
          },
        },
      });
    });
  });

  it("revokes approved policy bypass access with a design system dialog", async () => {
    const revokeRequest = vi.fn().mockResolvedValue({});
    mockPolicyBypassLists({
      approved: [
        policyBypassRequest({
          id: "request-revoke",
          label: "Revoke me",
          serverURL: "https://revoke.example.com/mcp",
          status: "approved",
        }),
      ],
    });
    mockAdminRBAC();
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: revokeRequest,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Revoke me")).toBeTruthy();
    });
    const row = screen.getByText("Revoke me").closest("tr");
    if (!row) throw new Error("Approved request row not found");
    fireEvent.click(within(row).getByRole("button", { name: "Revoke" }));

    await waitFor(() => {
      expect(screen.getByRole("dialog")).toBeTruthy();
    });
    expect(screen.getByRole("heading", { name: "Revoke access" })).toBeTruthy();
    const dialog = screen.getByRole("dialog");
    const requestName = within(dialog).getByText("Revoke me");
    expect(requestName.tagName).toBe("CODE");
    expect(requestName.className).toContain("font-mono");
    expect(revokeRequest).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

    await waitFor(() => {
      expect(revokeRequest).toHaveBeenCalledWith({
        request: { riskIDRequestBody: { id: "request-revoke" } },
      });
    });
  });

  it("resets pending access revocation when the project changes", async () => {
    mockAdminRBAC();
    mocks.usePolicyBypassRequests.mockImplementation(
      ({ status }: { status?: RiskPolicyBypassRequest["status"] }) => ({
        data: {
          requests:
            status === "approved"
              ? [
                  policyBypassRequest({
                    id: "request-project",
                    label: "Project access",
                    serverURL: "https://project.example.com/mcp",
                    status: "approved",
                  }),
                ]
              : [],
        },
        isLoading: false,
        error: null,
      }),
    );
    const queryClient = new QueryClient();
    const renderWithProject = (projectId: string) => (
      <MemoryRouter
        initialEntries={["/speakeasy/projects/project-1/approval-requests"]}
      >
        <Routes>
          <Route
            path="/:orgSlug/projects/:projectSlug/approval-requests"
            element={
              <QueryClientProvider client={queryClient}>
                <ApprovalRequestsContent projectId={projectId} />
              </QueryClientProvider>
            }
          />
        </Routes>
      </MemoryRouter>
    );

    const view = render(renderWithProject("project-1"));

    await waitFor(() => {
      expect(screen.getByText("Project access")).toBeTruthy();
    });
    const row = screen.getByText("Project access").closest("tr");
    if (!row) throw new Error("Approved request row not found");
    fireEvent.click(within(row).getByRole("button", { name: "Revoke" }));

    await waitFor(() => {
      expect(screen.getByRole("dialog")).toBeTruthy();
    });

    view.rerender(renderWithProject("project-2"));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });
});

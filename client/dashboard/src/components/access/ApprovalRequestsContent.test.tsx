import {
  fireEvent,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useRBAC: vi.fn(),
  useRoles: vi.fn(),
  useApprovalRequests: vi.fn(),
  useAccessRules: vi.fn(),
  useSdkClient: vi.fn(),
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

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: mocks.useSdkClient,
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  invalidateAllRoles: mocks.invalidate,
  useRoles: mocks.useRoles,
}));

vi.mock("@gram/client/react-query/shadowMCPApprovalRequests.js", () => ({
  invalidateAllShadowMCPApprovalRequests: mocks.invalidate,
  useShadowMCPApprovalRequests: mocks.useApprovalRequests,
}));

vi.mock("@gram/client/react-query/shadowMCPAccessRules.js", () => ({
  invalidateAllShadowMCPAccessRules: mocks.invalidate,
  useShadowMCPAccessRules: mocks.useAccessRules,
}));

vi.mock("@gram/client/react-query/approveShadowMCPApprovalRequest.js", () => ({
  useApproveShadowMCPApprovalRequestMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/denyShadowMCPApprovalRequest.js", () => ({
  useDenyShadowMCPApprovalRequestMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/createShadowMCPAccessRule.js", () => ({
  useCreateShadowMCPAccessRuleMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/updateShadowMCPAccessRule.js", () => ({
  useUpdateShadowMCPAccessRuleMutation: mocks.mutation,
}));

vi.mock("@gram/client/react-query/deleteShadowMCPAccessRule.js", () => ({
  useDeleteShadowMCPAccessRuleMutation: mocks.mutation,
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
  }) => <button onClick={onSelect}>{children}</button>,
  DropdownMenuTrigger: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  Table: ({
    columns,
    data,
    rowKey,
    noResultsMessage,
  }: {
    columns: Array<{
      header: ReactNode;
      key: string;
      render?: (row: Record<string, unknown>) => ReactNode;
    }>;
    data: Array<Record<string, unknown>>;
    rowKey: (row: Record<string, unknown>) => string;
    noResultsMessage?: ReactNode;
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
        {data.length === 0 && noResultsMessage ? (
          <tr>
            <td colSpan={columns.length}>{noResultsMessage}</td>
          </tr>
        ) : (
          data.map((row) => (
            <tr key={rowKey(row)}>
              {columns.map((column) => (
                <td key={column.key}>{column.render?.(row)}</td>
              ))}
            </tr>
          ))
        )}
      </tbody>
    </table>
  ),
}));

vi.mock("@/components/ui/select", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  const SelectContext = React.createContext<
    ((value: string) => void) | undefined
  >(undefined);

  return {
    Select: ({
      children,
      onValueChange,
    }: {
      children: ReactNode;
      onValueChange?: (value: string) => void;
    }) => (
      <SelectContext.Provider value={onValueChange}>
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
      const onValueChange = React.useContext(SelectContext);
      return <button onClick={() => onValueChange?.(value)}>{children}</button>;
    },
    SelectTrigger: ({ children }: { children: ReactNode }) => (
      <button>{children}</button>
    ),
    SelectValue: () => <span />,
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

vi.mock("@/components/ui/checkbox", () => ({
  Checkbox: () => <input type="checkbox" />,
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

vi.mock("@/components/moon/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => (
    <input {...props} />
  ),
}));

vi.mock("@/components/moon/textarea", () => ({
  Textarea: (props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) => (
    <textarea {...props} />
  ),
}));

import { ApprovalRequestsContent } from "./ApprovalRequestsContent";

function renderContent(projectId = "project-1") {
  const queryClient = new QueryClient();

  return render(
    <QueryClientProvider client={queryClient}>
      <ApprovalRequestsContent projectId={projectId} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mocks.useRBAC.mockReset();
  mocks.useSdkClient.mockReturnValue({
    access: {
      listShadowMCPApprovalRequests: vi.fn().mockResolvedValue({
        requests: [],
      }),
      listShadowMCPAccessRules: vi.fn().mockResolvedValue({
        rules: [],
      }),
    },
  });
  mocks.useRoles.mockReturnValue({ data: { roles: [] } });
  mocks.useApprovalRequests.mockReturnValue({
    data: { requests: [] },
    isLoading: false,
    error: null,
  });
  mocks.useAccessRules.mockReturnValue({
    data: { rules: [] },
    isLoading: false,
    error: null,
  });
  mocks.mutation.mockReturnValue({
    isPending: false,
    mutateAsync: vi.fn(),
  });
});

afterEach(() => {
  cleanup();
});

describe("ApprovalRequestsContent", () => {
  it("renders first-run empty states for approval requests and access rules", async () => {
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("No approval requests")).toBeTruthy();
    });
    expect(
      screen.getByText(
        "Requests will appear here when users ask for access after a policy block.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("No access rules")).toBeTruthy();
    expect(
      screen.getByText(
        "Create a rule manually or approve a request to allow or deny matching resources.",
      ),
    ).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Add Rule" })).toHaveLength(1);
    expect(screen.queryByText("Requested")).toBeNull();
    expect(screen.queryByText("Approved")).toBeNull();
    expect(screen.queryByText("All")).toBeNull();
    expect(screen.queryByText("Approval History")).toBeNull();
  });

  it("does not load or render approval requests for non-admin org readers", () => {
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:read",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:read"),
      hasAllScopes: () => false,
      isLoading: false,
    });

    renderContent();

    expect(
      mocks.useSdkClient().access.listShadowMCPApprovalRequests,
    ).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("heading", { name: "Approval Requests" }),
    ).toBeNull();
    expect(screen.getByRole("heading", { name: "Access Rules" })).toBeTruthy();
  });

  it("does not load project-scoped data without a project id", () => {
    const listShadowMCPApprovalRequests = vi.fn().mockResolvedValue({
      requests: [],
    });
    const listShadowMCPAccessRules = vi.fn().mockResolvedValue({
      rules: [],
    });
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });

    renderContent("");

    expect(listShadowMCPApprovalRequests).not.toHaveBeenCalled();
    expect(listShadowMCPAccessRules).not.toHaveBeenCalled();
  });

  it("loads additional approval request pages with the next cursor", async () => {
    const listShadowMCPApprovalRequests = vi
      .fn()
      .mockImplementation(
        ({ cursor, status }: { cursor?: string; status?: string }) => {
          if (status !== "requested") {
            return Promise.resolve({ requests: [] });
          }

          if (cursor === "next-requests") {
            return Promise.resolve({
              requests: [
                {
                  id: "request-2",
                  observedName: "Second request",
                  resourceType: "shadow_mcp",
                  requesterEmail: "second@example.com",
                  status: "requested",
                  blockedCount: 1,
                  lastBlockedAt: new Date("2026-01-02"),
                },
              ],
            });
          }

          return Promise.resolve({
            nextCursor: "next-requests",
            requests: [
              {
                id: "request-1",
                observedName: "First request",
                resourceType: "shadow_mcp",
                requesterEmail: "first@example.com",
                status: "requested",
                blockedCount: 1,
                lastBlockedAt: new Date("2026-01-01"),
              },
            ],
          });
        },
      );
    const listShadowMCPAccessRules = vi.fn().mockResolvedValue({
      rules: [],
    });
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });

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
    expect(within(requestsSection).getByText("Shadow MCP")).toBeTruthy();
    fireEvent.click(
      within(requestsSection).getByRole("button", { name: "Load more" }),
    );
    await waitFor(() => {
      expect(screen.getAllByText("Second request").length).toBeGreaterThan(0);
    });

    await waitFor(() => {
      expect(listShadowMCPApprovalRequests).toHaveBeenCalledWith({
        limit: 100,
        projectId: "project-1",
        status: "requested",
        cursor: "next-requests",
      });
    });
    expect(
      listShadowMCPApprovalRequests.mock.calls.map(
        ([request]) => request.status,
      ),
    ).not.toContain("approved");
    expect(
      listShadowMCPApprovalRequests.mock.calls.map(
        ([request]) => request.status,
      ),
    ).not.toContain("denied");
  });

  it("allows denying without a deny rule after clearing the rule name", async () => {
    const denyRequest = vi.fn().mockResolvedValue({});
    const listShadowMCPApprovalRequests = vi
      .fn()
      .mockImplementation(({ status }: { status?: string }) => {
        if (status !== "requested") {
          return Promise.resolve({ requests: [] });
        }

        return Promise.resolve({
          requests: [
            {
              id: "request-deny",
              observedName: "Denied without rule",
              observedFullUrl: "https://blocked.example.com/mcp",
              resourceType: "shadow_mcp",
              requesterEmail: "requester@example.com",
              projectId: "project-1",
              status: "requested",
              blockedCount: 1,
              lastBlockedAt: new Date("2026-01-01"),
            },
          ],
        });
      });
    const listShadowMCPAccessRules = vi.fn().mockResolvedValue({
      rules: [],
    });
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: denyRequest,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Denied without rule")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    expect(screen.queryByText("Project")).toBeNull();
    fireEvent.click(screen.getAllByRole("radio")[1]);
    fireEvent.change(screen.getAllByLabelText("Rule name")[0], {
      target: { value: "" },
    });

    const submitButton = screen.getByRole("button", { name: "Deny request" });
    expect(submitButton).toHaveProperty("disabled", false);
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(denyRequest).toHaveBeenCalledWith({
        request: {
          denyShadowMCPApprovalRequestForm: expect.objectContaining({
            id: "request-deny",
            createDenyRule: false,
            projectIds: ["project-1"],
            displayName: "",
          }),
        },
      });
    });
  });

  it("approves requests with the active project id", async () => {
    const approveRequest = vi.fn().mockResolvedValue({});
    const listShadowMCPApprovalRequests = vi
      .fn()
      .mockImplementation(({ status }: { status?: string }) => {
        if (status !== "requested") {
          return Promise.resolve({ requests: [] });
        }

        return Promise.resolve({
          requests: [
            {
              id: "request-approve",
              observedName: "Approve project request",
              observedFullUrl: "https://allowed.example.com/mcp",
              resourceType: "shadow_mcp",
              requesterEmail: "requester@example.com",
              projectId: "project-1",
              status: "requested",
              blockedCount: 1,
              lastBlockedAt: new Date("2026-01-01"),
            },
          ],
        });
      });
    const listShadowMCPAccessRules = vi.fn().mockResolvedValue({
      rules: [],
    });
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: approveRequest,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Approve project request")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Approve and create rule" }),
    );

    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith({
        request: {
          approveShadowMCPApprovalRequestForm: expect.objectContaining({
            id: "request-approve",
            accessScope: "project",
            projectIds: ["project-1"],
            matchBreadth: "full_url",
            matchValue: "https://allowed.example.com/mcp",
          }),
        },
      });
    });
  });

  it("creates manual rules with the active project id", async () => {
    const createRule = vi.fn().mockResolvedValue({});
    const listShadowMCPApprovalRequests = vi.fn().mockResolvedValue({
      requests: [],
    });
    const listShadowMCPAccessRules = vi.fn().mockResolvedValue({
      rules: [
        {
          id: "rule-existing",
          displayName: "Existing rule",
          resourceType: "shadow_mcp",
          disposition: "allowed",
          accessScope: "project",
          projectId: "project-1",
          matchBreadth: "url_host",
          matchValue: "existing.example.com",
          updatedAt: new Date("2026-01-01"),
        },
      ],
    });
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });
    mocks.mutation.mockReturnValue({
      isPending: false,
      mutateAsync: createRule,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Existing rule")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Add Rule" }));
    expect(screen.queryByText("Select project")).toBeNull();
    fireEvent.change(screen.getByLabelText("Rule name"), {
      target: { value: "New project rule" },
    });
    fireEvent.change(screen.getByLabelText("Match value"), {
      target: { value: "https://new.example.com/mcp" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add rule" }));

    await waitFor(() => {
      expect(createRule).toHaveBeenCalledWith({
        request: {
          createShadowMCPAccessRuleForm: expect.objectContaining({
            displayName: "New project rule",
            accessScope: "project",
            projectIds: ["project-1"],
            matchBreadth: "full_url",
            matchValue: "https://new.example.com/mcp",
          }),
        },
      });
    });
  });

  it("loads additional access rule pages with the next cursor", async () => {
    const listShadowMCPApprovalRequests = vi.fn().mockResolvedValue({
      requests: [],
    });
    const listShadowMCPAccessRules = vi
      .fn()
      .mockImplementation(({ cursor }: { cursor?: string }) => {
        if (cursor === "next-rules") {
          return Promise.resolve({
            rules: [
              {
                id: "rule-2",
                displayName: "Second rule",
                resourceType: "shadow_mcp",
                disposition: "allowed",
                matchBreadth: "url_host",
                matchValue: "second.example.com",
                updatedAt: new Date("2026-01-02"),
              },
            ],
          });
        }

        return Promise.resolve({
          nextCursor: "next-rules",
          rules: [
            {
              id: "rule-1",
              displayName: "First rule",
              resourceType: "shadow_mcp",
              disposition: "allowed",
              matchBreadth: "url_host",
              matchValue: "first.example.com",
              updatedAt: new Date("2026-01-01"),
            },
          ],
        });
      });
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getAllByText("First rule").length).toBeGreaterThan(0);
    });
    expect(screen.getByRole("columnheader", { name: "Type" })).toBeTruthy();
    expect(screen.getByText("Shadow MCP")).toBeTruthy();
    const accessRuleHeadings = screen.getAllByRole("heading", {
      name: "Access Rules",
    });
    const accessRulesSection =
      accessRuleHeadings[accessRuleHeadings.length - 1]?.closest("section");
    if (!accessRulesSection) throw new Error("Access Rules section not found");
    fireEvent.click(
      within(accessRulesSection).getByRole("button", { name: "Load more" }),
    );
    await waitFor(() => {
      expect(screen.getAllByText("Second rule").length).toBeGreaterThan(0);
    });

    await waitFor(() => {
      expect(listShadowMCPAccessRules).toHaveBeenCalledWith({
        limit: 100,
        accessScope: "project",
        projectId: "project-1",
        disposition: undefined,
        cursor: "next-rules",
      });
    });
  });

  it("renders project-scoped blocked access rules", async () => {
    const listShadowMCPApprovalRequests = vi.fn().mockResolvedValue({
      requests: [],
    });
    const listShadowMCPAccessRules = vi
      .fn()
      .mockImplementation(
        ({
          accessScope,
          projectId,
        }: {
          accessScope?: string;
          projectId?: string;
        }) => {
          if (accessScope !== "project" || projectId !== "project-1") {
            return Promise.resolve({ rules: [] });
          }

          return Promise.resolve({
            rules: [
              {
                id: "rule-blocked-project",
                displayName: "Blocked project rule",
                resourceType: "shadow_mcp",
                disposition: "denied",
                accessScope: "project",
                projectId: "project-1",
                matchBreadth: "url_host",
                matchValue: "blocked.example.com",
                updatedAt: new Date("2026-01-01"),
              },
            ],
          });
        },
      );
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Blocked project rule")).toBeTruthy();
    });
    const blockedRuleRow = screen
      .getByText("Blocked project rule")
      .closest("tr");
    if (!blockedRuleRow) throw new Error("Blocked rule row not found");
    expect(within(blockedRuleRow).getByText("Denied")).toBeTruthy();
    expect(within(blockedRuleRow).getByText("Project")).toBeTruthy();
    expect(listShadowMCPAccessRules).toHaveBeenCalledWith({
      limit: 100,
      accessScope: "project",
      projectId: "project-1",
      disposition: undefined,
      cursor: undefined,
    });
  });

  it("renders a filtered empty state when a rule filter has no matches", async () => {
    const listShadowMCPApprovalRequests = vi.fn().mockResolvedValue({
      requests: [],
    });
    const listShadowMCPAccessRules = vi
      .fn()
      .mockImplementation(
        ({ disposition }: { disposition?: "allowed" | "denied" }) => {
          if (disposition === "denied") {
            return Promise.resolve({ rules: [] });
          }

          return Promise.resolve({
            rules: [
              {
                id: "rule-1",
                displayName: "Allowed rule",
                resourceType: "shadow_mcp",
                disposition: "allowed",
                matchBreadth: "url_host",
                matchValue: "allowed.example.com",
                updatedAt: new Date("2026-01-01"),
              },
            ],
          });
        },
      );
    mocks.useSdkClient.mockReturnValue({
      access: {
        listShadowMCPApprovalRequests,
        listShadowMCPAccessRules,
      },
    });
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:admin",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });

    renderContent();

    await waitFor(() => {
      expect(screen.getByText("Allowed rule")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Denied" }));

    await waitFor(() => {
      expect(screen.getByText("No matching rules")).toBeTruthy();
    });
    expect(screen.queryByText("No access rules")).toBeNull();
    expect(screen.getByRole("columnheader", { name: "Match" })).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "Status" })).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "Scope" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "All rules" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add Rule" })).toBeTruthy();
    expect(listShadowMCPAccessRules).toHaveBeenCalledWith({
      limit: 100,
      accessScope: "project",
      projectId: "project-1",
      disposition: "denied",
      cursor: undefined,
    });
  });
});

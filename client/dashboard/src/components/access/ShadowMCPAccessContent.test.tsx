import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useRBAC: vi.fn(),
  useRoles: vi.fn(),
  useApprovalRequests: vi.fn(),
  useAccessRules: vi.fn(),
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
  }: {
    columns: Array<{
      header: ReactNode;
      key: string;
      render?: (row: Record<string, unknown>) => ReactNode;
    }>;
    data: Array<Record<string, unknown>>;
    rowKey: (row: Record<string, unknown>) => string;
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

vi.mock("@/components/ui/select", () => ({
  Select: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SelectContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  SelectItem: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SelectTrigger: ({ children }: { children: ReactNode }) => (
    <button>{children}</button>
  ),
  SelectValue: () => <span />,
}));

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

vi.mock("@/components/ui/radio-group", () => ({
  RadioGroup: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  RadioGroupItem: () => <input type="radio" />,
}));

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

import { ShadowMCPAccessContent } from "./ShadowMCPAccessContent";

function renderContent() {
  const queryClient = new QueryClient();

  return render(
    <QueryClientProvider client={queryClient}>
      <ShadowMCPAccessContent />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mocks.useRBAC.mockReset();
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

describe("ShadowMCPAccessContent", () => {
  it("does not load or render approval requests for non-admin org readers", () => {
    mocks.useRBAC.mockReturnValue({
      hasScope: (scope: string) => scope === "org:read",
      hasAnyScope: (scopes: string[]) => scopes.includes("org:read"),
      hasAllScopes: () => false,
      isLoading: false,
    });

    renderContent();

    expect(mocks.useApprovalRequests).toHaveBeenCalledWith(
      {
        limit: 100,
        status: "requested",
      },
      undefined,
      { enabled: false },
    );
    expect(screen.queryByRole("heading", { name: "Requests" })).toBeNull();
    expect(screen.getByRole("heading", { name: "Access Rules" })).toBeTruthy();
  });
});

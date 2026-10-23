import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({
  admin: true,
  mutate: vi.fn(),
  query: vi.fn(),
}));
vi.mock("@/components/mcp/useBuiltinMcpServers", () => ({
  useBuiltinMcpServers: () => ({
    isAdmin: state.admin,
    servers: [{ id: "builtin-one", key: "slack", name: "Slack", hidden: true }],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-one",
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: (...args: unknown[]) => {
    state.query(...args);
    return {
      data: { environments: [{ id: "env-one", name: "Production" }] },
      isLoading: false,
      isError: false,
    };
  },
}));
vi.mock("@gram/client/react-query/metaMcpMembers.js", () => ({
  invalidateAllMetaMcpMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/metaMcpServers.js", () => ({
  invalidateAllMetaMcpServers: vi.fn(),
}));
vi.mock("@gram/client/react-query/setBuiltinMetaMcpMember.js", () => ({
  useSetBuiltinMetaMcpMemberMutation: () => ({
    mutate: state.mutate,
    isPending: false,
  }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@/components/ui/Select", () => ({
  Select: ({
    children,
    value,
    onValueChange,
    disabled,
  }: {
    children: ReactNode;
    value: string;
    onValueChange: (value: string) => void;
    disabled: boolean;
  }) => (
    <select
      value={value}
      onChange={(event) => onValueChange(event.target.value)}
      disabled={disabled}
    >
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: ReactNode }) => children,
  SelectItem: ({ children, value }: { children: ReactNode; value: string }) => (
    <option value={value}>{children}</option>
  ),
}));

import { BuiltinGatewayMembersSection } from "./BuiltinGatewayMembersSection";
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.admin = true;
});

it("adds a built-in using the selected project environment without granting connection access", () => {
  render(<BuiltinGatewayMembersSection gatewayId="gateway-one" members={[]} />);
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "env-one" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add to gateway" }));
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      gramProject: "project-one",
      setBuiltinMetaMcpMemberRequestBody: {
        metaMcpServerId: "gateway-one",
        builtinKey: "slack",
        environmentId: "env-one",
        sortOrder: 0,
      },
    },
  });
  expect(
    screen.getByText(/Membership does not grant connection access/),
  ).toBeTruthy();
  expect(state.query.mock.calls[0]?.[0]).toEqual({
    gramProject: "project-one",
  });
});
it("can clear an override without removing or reordering an existing hidden built-in membership", () => {
  render(
    <BuiltinGatewayMembersSection
      gatewayId="gateway-one"
      members={[
        {
          id: "member-one",
          mcpServerId: "builtin-one",
          builtinKey: "slack",
          environmentId: "env-one",
          sortOrder: 7,
        },
      ]}
    />,
  );
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "default" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save override" }));
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      gramProject: "project-one",
      setBuiltinMetaMcpMemberRequestBody: {
        metaMcpServerId: "gateway-one",
        builtinKey: "slack",
        environmentId: undefined,
        sortOrder: 7,
      },
    },
  });
});
it("does not offer built-in membership controls or environment reads to non-admins", () => {
  state.admin = false;
  render(<BuiltinGatewayMembersSection gatewayId="gateway-one" members={[]} />);
  expect(screen.queryByText("Built-in servers")).toBeNull();
  expect(state.query).not.toHaveBeenCalled();
});

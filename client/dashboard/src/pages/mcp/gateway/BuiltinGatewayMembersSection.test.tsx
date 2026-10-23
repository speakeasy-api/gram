import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({
  admin: true,
  canWrite: true,
  environmentsResolved: true,
  environmentsLoading: false,
  environmentsError: false,
  invalidateQueries: vi.fn(),
  onSuccess: undefined as undefined | (() => Promise<void>),
  mutate: vi.fn(),
  query: vi.fn(),
}));
vi.mock("@/components/mcp/useBuiltinMcpServers", () => ({
  useBuiltinMcpServers: () => ({
    isAdmin: state.admin,
    isError: false,
    isLoading: false,
    servers: [
      {
        id: "builtin-one",
        key: "slack",
        name: "Slack",
        hidden: true,
        tools: [{ name: "slack_send_message" }],
      },
    ],
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, id: string) =>
      (scope === "mcp:write" && id === "gateway-one" && state.canWrite) ||
      (scope === "org:admin" && id === "org-one" && state.admin),
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-one",
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      builtin: {
        overview: { href: (key: string) => `/mcp/builtin/${key}/overview` },
      },
    },
  }),
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: (...args: unknown[]) => {
    state.query(...args);
    return {
      data: state.environmentsResolved
        ? { environments: [{ id: "env-one", name: "Production" }] }
        : undefined,
      isLoading: state.environmentsLoading,
      isError: state.environmentsError,
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
  useSetBuiltinMetaMcpMemberMutation: (options: {
    onSuccess: () => Promise<void>;
  }) => {
    state.onSuccess = options.onSuccess;
    return { mutate: state.mutate, isPending: false };
  },
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: state.invalidateQueries }),
}));
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

import {
  BuiltinGatewayAddSection,
  BuiltinMemberEnvironmentCell,
} from "./BuiltinGatewayMembersSection";

const slackMember = {
  id: "member-one",
  mcpServerId: "builtin-one",
  mcpServerName: "Slack",
  builtinKey: "slack",
  environmentId: "env-one",
  sortOrder: 7,
};

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.admin = true;
  state.canWrite = true;
  state.environmentsResolved = true;
  state.environmentsLoading = false;
  state.environmentsError = false;
});

it("adds a built-in on the project default, after the existing members", () => {
  render(
    <BuiltinGatewayAddSection
      gatewayId="gateway-one"
      members={[{ ...slackMember, builtinKey: undefined, sortOrder: 4 }]}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Add" }));
  expect(state.mutate.mock.calls[0]?.[0]).toEqual({
    request: {
      gramProject: "project-one",
      setBuiltinMetaMcpMemberRequestBody: {
        metaMcpServerId: "gateway-one",
        builtinKey: "slack",
        environmentId: undefined,
        sortOrder: 5,
      },
    },
  });
});

it("stops offering a built-in once it is already a member", () => {
  render(
    <BuiltinGatewayAddSection
      gatewayId="gateway-one"
      members={[slackMember]}
    />,
  );
  expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
});

it("does not offer built-in membership to non-admins", () => {
  state.admin = false;
  render(<BuiltinGatewayAddSection gatewayId="gateway-one" members={[]} />);
  expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
});

it("does not offer built-in membership without gateway write", () => {
  state.canWrite = false;
  render(<BuiltinGatewayAddSection gatewayId="gateway-one" members={[]} />);
  expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
});

it("clears an override without removing or reordering the membership", () => {
  render(
    <BuiltinMemberEnvironmentCell
      gatewayId="gateway-one"
      member={slackMember}
      canWrite
    />,
  );
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "server-default" },
  });
  expect(state.mutate.mock.calls[0]?.[0]).toEqual({
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
  expect(state.query.mock.calls[0]?.[0]).toEqual({
    gramProject: "project-one",
  });
});

it("reads the override without a control when the viewer cannot write", () => {
  render(
    <BuiltinMemberEnvironmentCell
      gatewayId="gateway-one"
      member={slackMember}
      canWrite={false}
    />,
  );
  expect(screen.queryByRole("combobox")).toBeNull();
  expect(screen.getByText("Production")).toBeTruthy();
});

it.each([
  ["loading", { environmentsLoading: true }, "Loading…"],
  ["unavailable", { environmentsError: true }, "Unavailable"],
])(
  "does not read an unresolved override as the server default while %s",
  (_label, overrides, expected) => {
    state.environmentsResolved = false;
    Object.assign(state, overrides);
    render(
      <BuiltinMemberEnvironmentCell
        gatewayId="gateway-one"
        member={slackMember}
        canWrite={false}
      />,
    );
    expect(screen.getByText(expected)).toBeTruthy();
    expect(screen.queryByText("Server default")).toBeNull();
  },
);

it("reads a member without an override as the server default", () => {
  render(
    <BuiltinMemberEnvironmentCell
      gatewayId="gateway-one"
      member={{ ...slackMember, environmentId: undefined }}
      canWrite={false}
    />,
  );
  expect(screen.getByText("Server default")).toBeTruthy();
});

it("refreshes inspection caches after a membership write", async () => {
  render(<BuiltinGatewayAddSection gatewayId="gateway-one" members={[]} />);
  await state.onSuccess!();
  expect(state.invalidateQueries).toHaveBeenCalledWith({
    queryKey: ["gatewayInspection"],
  });
  expect(state.invalidateQueries).toHaveBeenCalledWith({
    queryKey: ["gatewayDescribeServer"],
  });
});

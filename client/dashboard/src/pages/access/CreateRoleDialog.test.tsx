import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { CreateRoleDialog } from "./CreateRoleDialog";
import type { Role } from "@gram/client/models/components/role.js";

const mocks = vi.hoisted(() => ({
  status: "ready" as "ready" | "loading" | "error",
  enabled: false as boolean | undefined,
  agents: vi.fn(() => ({ data: [] })),
  create: vi.fn(),
  update: vi.fn(),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetryContext: () => ({
    featureFlags: { status: mocks.status },
    telemetry: { isFeatureEnabled: () => mocks.enabled },
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ projects: [] }),
}));
vi.mock("@/routes", () => ({ useOrgRoutes: () => ({}) }));
vi.mock("@gram/client/react-query/agents.js", () => ({
  useAgents: mocks.agents,
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
  invalidateAllMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/listScopes.js", () => ({
  useListScopes: () => ({
    data: {
      scopes: [
        {
          slug: "org:read",
          resourceType: "org",
          visibility: "user_visible",
          label: "Read organization",
        },
        {
          slug: "mcp:connect",
          resourceType: "mcp",
          visibility: "user_visible",
          label: "Connect to MCP servers",
        },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/createRole.js", () => ({
  useCreateRoleMutation: () => ({ mutate: mocks.create, isPending: false }),
}));
vi.mock("@gram/client/react-query/updateRole.js", () => ({
  useUpdateRoleMutation: () => ({ mutate: mocks.update, isPending: false }),
}));
vi.mock("./RolePermissionsSection", () => ({
  RolePermissionsSection: ({
    onToggleScope,
    renderScopeRule,
  }: {
    onToggleScope: (scope: "org:read" | "mcp:connect") => void;
    renderScopeRule: (scope: {
      slug: "mcp:connect";
      resourceType: "mcp";
    }) => React.ReactNode;
  }) => (
    <>
      <button onClick={() => onToggleScope("org:read")}>
        Read organization
      </button>
      <button onClick={() => onToggleScope("mcp:connect")}>
        Connect to MCP servers
      </button>
      {renderScopeRule({ slug: "mcp:connect", resourceType: "mcp" })}
    </>
  ),
}));
vi.mock("./PermissionScopeControl", () => ({
  PermissionScopeControl: ({
    onChooseSpecific,
    onResetToAll,
  }: {
    onChooseSpecific: () => void;
    onResetToAll: () => void;
  }) => (
    <>
      <button onClick={onChooseSpecific}>Choose specific servers</button>
      <button onClick={onResetToAll}>Reset to all servers</button>
    </>
  ),
}));
vi.mock("./GrantRuleDrawerContent", () => ({
  GrantRuleDrawerContent: ({
    onChangeSelectors,
  }: {
    onChangeSelectors: (
      selectors: Array<{ resourceKind: string; resourceId: string }>,
    ) => void;
  }) => (
    <button
      onClick={() =>
        onChangeSelectors([{ resourceKind: "mcp", resourceId: "server-a" }])
      }
    >
      Select server A
    </button>
  ),
}));

const role: Role = {
  id: "role-test",
  name: "Test role",
  description: "Original description",
  agentIds: ["agent-test"],
  grants: [],
  isSystem: false,
  memberCount: 0,
  principalUrn: "test-role",
  slug: "test-role",
  createdAt: new Date(),
  updatedAt: new Date(),
};
function renderEditor(editingRole?: Role, confirmAssignmentFor?: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <CreateRoleDialog
        open
        onOpenChange={vi.fn<(open: boolean) => void>()}
        editingRole={editingRole}
        confirmAssignmentFor={confirmAssignmentFor}
        presentation="page"
      />
    </QueryClientProvider>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.status = "ready";
  mocks.enabled = false;
});
it("labels the permissions section as a section rather than an action", () => {
  renderEditor();
  expect(screen.getByText("Permissions", { exact: true })).toBeTruthy();
});

describe("role assignment confirmation", () => {
  function confirmAssignment() {
    const confirmation = screen.getByRole("checkbox", {
      name: "Confirm role assignment",
    });
    fireEvent.click(confirmation);
    expect(confirmation.getAttribute("data-state")).toBe("checked");
    return confirmation;
  }

  it("clears an acknowledgement when a scope is toggled", () => {
    renderEditor(undefined, "Denied User");
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Read organization" }));

    const confirmation = confirmAssignment();
    fireEvent.click(screen.getByRole("button", { name: "Read organization" }));

    expect(confirmation.getAttribute("data-state")).toBe("unchecked");
  });

  it("clears an acknowledgement when a rule-editor change is saved", () => {
    renderEditor(undefined, "Denied User");
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Connect to MCP servers" }),
    );

    const confirmation = confirmAssignment();
    fireEvent.click(
      screen.getByRole("button", { name: "Choose specific servers" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Select server A" }));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    expect(confirmation.getAttribute("data-state")).toBe("unchecked");
  });

  it("clears an acknowledgement when a rule is reset to all resources", () => {
    renderEditor(undefined, "Denied User");
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Connect to MCP servers" }),
    );

    const confirmation = confirmAssignment();
    fireEvent.click(
      screen.getByRole("button", { name: "Reset to all servers" }),
    );

    expect(confirmation.getAttribute("data-state")).toBe("unchecked");
  });
});

describe("agent management rollout", () => {
  it.each(["false", "loading", "missing", "error"] as const)(
    "does not enable the query or show the picker when %s",
    (state) => {
      if (state === "loading" || state === "error") mocks.status = state;
      if (state === "missing") mocks.enabled = undefined;
      renderEditor();
      expect(mocks.agents).toHaveBeenLastCalledWith(undefined, undefined, {
        enabled: false,
        throwOnError: false,
      });
      expect(screen.queryByText("Assign Agents")).toBeNull();
    },
  );
  it("enables the query and picker when enabled", () => {
    mocks.enabled = true;
    renderEditor();
    expect(mocks.agents).toHaveBeenLastCalledWith(undefined, undefined, {
      enabled: true,
      throwOnError: false,
    });
    fireEvent.click(screen.getByText("Assign Agents"));
    expect(
      screen.getByText("No agents in this organization yet."),
    ).toBeTruthy();
  });
  it("saves an ordinary role without replacing existing agents when unavailable", () => {
    renderEditor(role);
    fireEvent.change(
      screen.getByPlaceholderText("Describe what this role can do..."),
      { target: { value: "Updated description" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(mocks.update).toHaveBeenCalledOnce();
    const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
    expect(form.description).toBe("Updated description");
    expect(form).not.toHaveProperty("agentIds");
  });
  it("creates an ordinary role without agent assignments when unavailable", () => {
    renderEditor();
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Test role" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Read organization" }));
    fireEvent.click(screen.getByRole("button", { name: "Create Role" }));
    expect(mocks.create).toHaveBeenCalledOnce();
    expect(
      mocks.create.mock.calls[0]![0].request.createRoleForm,
    ).not.toHaveProperty("agentIds");
  });
});

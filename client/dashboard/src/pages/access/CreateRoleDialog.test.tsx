import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { CreateRoleDialog } from "./CreateRoleDialog";
import type { Role } from "@gram/client/models/components/role.js";

const mocks = vi.hoisted(() => ({
  agents: vi.fn(() => ({ data: [] })),
  create: vi.fn(),
  update: vi.fn(),
  scimEnabled: false,
  members: [] as {
    id: string;
    name: string;
    email: string;
    roleIds: string[];
  }[],
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ projects: [], scimEnabled: mocks.scimEnabled }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
}));
vi.mock("@gram/client/react-query/agents.js", () => ({
  useAgents: mocks.agents,
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: mocks.members } }),
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
          exclusionScope: "mcp:blocked_connect",
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
// The MCP access tab reads the organization's servers; the section that
// would show it is mocked below.
vi.mock("./useMcpAccessCount", () => ({ useMcpAccessCount: () => 0 }));
vi.mock("./useOrgMcpServers", () => ({
  useOrgMcpServers: () => ({
    groups: [
      {
        projectId: "project-a",
        projectName: "Default",
        servers: [
          { id: "server-a", name: "GitHub", slug: "github" },
          { id: "server-b", name: "Linear", slug: "linear" },
        ],
      },
    ],
    settled: true,
    isError: false,
    refetch: vi.fn(),
  }),
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
vi.mock("./PermissionScopeControl", async (importActual) => ({
  // The block-only row is what these tests read, so it renders for real.
  BlockedScopeControl: (
    await importActual<typeof import("./PermissionScopeControl")>()
  ).BlockedScopeControl,
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
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <CreateRoleDialog
          open
          onOpenChange={vi.fn<(open: boolean) => void>()}
          editingRole={editingRole}
          confirmAssignmentFor={confirmAssignmentFor}
          presentation="page"
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.scimEnabled = false;
  mocks.members = [];
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

describe("agent assignment", () => {
  it("requests agents and shows the picker", () => {
    renderEditor();
    expect(mocks.agents).toHaveBeenLastCalledWith(undefined, undefined, {
      throwOnError: false,
    });
    fireEvent.click(screen.getByText("Assign Agents"));
    expect(
      screen.getByText("No agents in this organization yet."),
    ).toBeTruthy();
  });
  it("keeps an edited role's existing agents when nothing about them changes", () => {
    renderEditor(role);
    fireEvent.change(
      screen.getByPlaceholderText("Describe what this role can do..."),
      { target: { value: "Updated description" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(mocks.update).toHaveBeenCalledOnce();
    const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
    expect(form.description).toBe("Updated description");
    expect(form.agentIds).toEqual(["agent-test"]);
  });
  it("creates a role without agent assignments when none are selected", () => {
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

describe("member assignment under directory sync", () => {
  beforeEach(() => {
    mocks.scimEnabled = true;
    mocks.members = [
      {
        id: "m-1",
        name: "Enrolled Person",
        email: "a@example.com",
        roleIds: [role.id],
      },
      { id: "m-2", name: "Other Person", email: "b@example.com", roleIds: [] },
    ];
  });

  it("lists only the members the directory enrolled", () => {
    renderEditor(role);
    fireEvent.click(screen.getByText("Assign Members"));
    expect(screen.getByText("Enrolled Person")).toBeTruthy();
    expect(screen.queryByText("Other Person")).toBeNull();
    expect(
      screen
        .getByRole("checkbox", { name: /Enrolled Person/ })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("links to directory sync setup when nobody is enrolled", () => {
    renderEditor();
    const link = screen.getByRole("link", { name: "Configure directory sync" });
    expect(link.getAttribute("href")).toBe("/org/identity");
    expect(screen.queryByText("Enrolled Person")).toBeNull();
  });
});

describe("a permission the role only blocks", () => {
  const blockingRole: Role = {
    ...role,
    grants: [
      {
        scope: "mcp:blocked_connect",
        selectors: [{ resourceKind: "mcp", resourceId: "server-b" }],
      },
    ],
  };

  it("names the blocked server instead of reading as a grant", () => {
    renderEditor(blockingRole);
    expect(screen.getByRole("button", { name: "Linear" })).toBeTruthy();
    expect(
      screen.getByText(/Applies even when another role grants it/),
    ).toBeTruthy();
  });

  it("keeps the block when the permission is granted beside it", () => {
    renderEditor(blockingRole);
    fireEvent.click(
      screen.getByRole("button", { name: "Connect to MCP servers" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(mocks.update).toHaveBeenCalledOnce();
    const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
    expect(form.addGrants).toEqual([
      expect.objectContaining({ scope: "mcp:connect" }),
    ]);
    expect(form.removeGrants ?? []).toEqual([]);
  });

  it("edits which server a block covers without granting anything", () => {
    renderEditor(blockingRole);
    fireEvent.click(screen.getByRole("button", { name: "Linear" }));
    fireEvent.click(screen.getByRole("button", { name: "Select server A" }));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
    expect(form.addGrants).toEqual([
      {
        scope: "mcp:blocked_connect",
        selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
      },
    ]);
    expect(form.removeGrants).toEqual([
      {
        scope: "mcp:blocked_connect",
        selectors: [{ resourceKind: "mcp", resourceId: "server-b" }],
      },
    ]);
  });
});

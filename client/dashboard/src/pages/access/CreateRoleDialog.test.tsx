import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";

import { CreateRoleDialog } from "./CreateRoleDialog";
import type { Role } from "@gram/client/models/components/role.js";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  scimEnabled: false,
  members: [] as {
    id: string;
    name: string;
    email: string;
    roleIds: string[];
    groups?: string[];
  }[],
  mappings: {
    groups: [] as { id: string; name: string; memberCount: number }[],
    attributes: [] as { key: string; value: string; memberCount: number }[],
    mappings: [] as {
      id: string;
      sourceKind: "group" | "attribute";
      roleUrn: string;
      directoryGroupId?: string;
    }[],
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "project-a", name: "Default", slug: "default" }],
    scimEnabled: mocks.scimEnabled,
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
}));
vi.mock("@gram/client/react-query/agents.js", () => ({
  queryKeyAgents: () => ["agents"],
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { list: mocks.list } }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: mocks.members } }),
  invalidateAllMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/directoryRoleMappings.js", () => ({
  useDirectoryRoleMappings: () => ({
    data: mocks.mappings,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  }),
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
          slug: "project:read",
          resourceType: "project",
          visibility: "user_visible",
          description: "Read projects",
          exclusionScope: "project:blocked_read",
        },
        {
          slug: "inference:execute",
          resourceType: "inference",
          visibility: "user_visible",
          description: "Run inference",
        },
        ...(["connect", "read", "write"] as const).map((action) => ({
          slug: `mcp:${action}`,
          resourceType: "mcp",
          visibility: "user_visible",
          exclusionScope: `mcp:blocked_${action}`,
        })),
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
        onChangeSelectors([
          { resourceKind: "project", resourceId: "project-a" },
        ])
      }
    >
      Select project A
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
  const client = new QueryClient();
  return {
    client,
    ...render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <TooltipProvider>
            <CreateRoleDialog
              open
              onOpenChange={vi.fn<(open: boolean) => void>()}
              editingRole={editingRole}
              confirmAssignmentFor={confirmAssignmentFor}
              presentation="page"
            />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    ),
  };
}
// Radix tabs switch on mouse down, not click.
function openPlatformTab() {
  fireEvent.mouseDown(screen.getByRole("tab", { name: "Platform access" }));
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.scimEnabled = false;
  mocks.members = [];
  // agents.list is paginated: it resolves to pages, which the dialog drains.
  mocks.list.mockResolvedValue([{ result: { items: [] } }]);
  mocks.mappings = { groups: [], attributes: [], mappings: [] };
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

  it("clears an acknowledgement when a cell is toggled", () => {
    renderEditor(undefined, "Denied User");
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    openPlatformTab();
    fireEvent.click(screen.getByRole("button", { name: "View organization" }));

    const confirmation = confirmAssignment();
    fireEvent.click(screen.getByRole("button", { name: "View organization" }));

    expect(confirmation.getAttribute("data-state")).toBe("unchecked");
  });

  it("clears an acknowledgement when a rule-editor change is saved", () => {
    renderEditor(undefined, "Denied User");
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    openPlatformTab();
    fireEvent.click(screen.getByRole("button", { name: "View projects" }));

    const confirmation = confirmAssignment();
    fireEvent.click(
      screen.getByRole("button", { name: "Change where View applies" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Select project A" }));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    expect(confirmation.getAttribute("data-state")).toBe("unchecked");
  });
});

describe("agent assignment", () => {
  it("reads every agent page and shows the picker", async () => {
    // Two pages: a picker that drained only the first would still look
    // healthy with one, and dropping an agent from it is silent.
    mocks.list.mockResolvedValue([
      {
        result: {
          items: [
            { id: "agent_first", name: "First agent", lifecycle: "active" },
          ],
        },
      },
      {
        result: {
          items: [
            { id: "agent_second", name: "Second agent", lifecycle: "active" },
          ],
        },
      },
    ]);
    renderEditor();
    await waitFor(() => expect(mocks.list).toHaveBeenCalled());
    fireEvent.click(screen.getByText("Assign Agents"));
    expect(await screen.findByText("First agent")).toBeTruthy();
    expect(screen.getByText("Second agent")).toBeTruthy();
  });

  it("shows the empty state when the organization has no agents", async () => {
    const { client } = renderEditor();
    await waitFor(() => expect(mocks.list).toHaveBeenCalled());
    // The picker renders this same message while the query is still in
    // flight, so an assertion made now would pass without the fetch
    // resolving at all. Wait for the empty page to land first.
    await waitFor(() => expect(client.isFetching()).toBe(0));
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
    openPlatformTab();
    fireEvent.click(screen.getByRole("button", { name: "View organization" }));
    fireEvent.click(screen.getByRole("button", { name: "Create Role" }));
    expect(mocks.create).toHaveBeenCalledOnce();
    expect(
      mocks.create.mock.calls[0]![0].request.createRoleForm,
    ).not.toHaveProperty("agentIds");
  });
});

describe("who holds the role under directory sync", () => {
  beforeEach(() => {
    mocks.scimEnabled = true;
    mocks.mappings = {
      groups: [{ id: "g-data", name: "Data Analytics", memberCount: 14 }],
      attributes: [],
      mappings: [
        {
          id: "map-1",
          sourceKind: "group",
          roleUrn: role.principalUrn,
          directoryGroupId: "g-data",
        },
      ],
    };
  });

  it("shows the groups that give out the role in place of a member picker", () => {
    renderEditor(role);
    expect(screen.getByText("Data Analytics")).toBeTruthy();
    expect(screen.getByText("14 users")).toBeTruthy();
    expect(screen.queryByText("Assign Members")).toBeNull();
    expect(
      screen.getByRole("link", { name: "View mapping →" }).getAttribute("href"),
    ).toBe("/org/identity");
  });

  it("says so when nothing gives out the role", () => {
    mocks.mappings.mappings = [];
    renderEditor(role);
    expect(
      screen.getByText("No directory group or attribute gives out this role"),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Map a group" }).getAttribute("href"),
    ).toBe("/org/identity");
  });

  it("points a new role at mapping once it exists", () => {
    renderEditor();
    expect(screen.getByRole("link", { name: "IDP and SSO" })).toBeTruthy();
    expect(screen.queryByText("Assign Members")).toBeNull();
  });
});

describe("the access grid", () => {
  /** The default project, which the servers sit in, starts open. */
  function show(editingRole: Parameters<typeof renderEditor>[0]) {
    renderEditor(editingRole);
  }

  function savedForm() {
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(mocks.update).toHaveBeenCalledOnce();
    return mocks.update.mock.calls[0]![0].request.updateRoleForm;
  }

  it("grants one level on one server", () => {
    show(role);
    fireEvent.click(screen.getByRole("button", { name: "Manage GitHub" }));
    // Manage includes the levels below it, which the role does not store.
    expect(
      screen
        .getByRole("button", { name: "Connect GitHub" })
        .getAttribute("aria-pressed"),
    ).toBe("false");
    expect(savedForm().addGrants).toEqual([
      {
        scope: "mcp:write",
        selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
      },
    ]);
  });

  it("shows a block the role stores without a grant beside it", () => {
    show({
      ...role,
      grants: [
        {
          scope: "mcp:blocked_connect",
          selectors: [{ resourceKind: "mcp", resourceId: "server-b" }],
        },
      ],
    });
    expect(
      screen.getByRole("button", { name: "Connect Linear, blocked" }),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "View Linear" })
        .getAttribute("aria-pressed"),
    ).toBe("false");
  });

  it("blocks every level on a server from the Blocked column", () => {
    show({
      ...role,
      grants: [
        {
          scope: "mcp:connect",
          selectors: [
            { resourceKind: "mcp", resourceId: "server-a" },
            { resourceKind: "mcp", resourceId: "server-b" },
          ],
        },
      ],
    });
    fireEvent.click(screen.getByRole("button", { name: "Block Linear" }));
    const form = savedForm();
    const linear = [{ resourceKind: "mcp", resourceId: "server-b" }];
    expect(form.addGrants).toEqual(
      expect.arrayContaining([
        { scope: "mcp:blocked_connect", selectors: linear },
        { scope: "mcp:blocked_read", selectors: linear },
        { scope: "mcp:blocked_write", selectors: linear },
      ]),
    );
    expect(form.removeGrants).toEqual([
      { scope: "mcp:connect", selectors: linear },
    ]);
  });

  it("saves an untouched grid without changing any grant", () => {
    show({
      ...role,
      grants: [
        { scope: "mcp:connect" },
        {
          scope: "mcp:read",
          selectors: [{ resourceKind: "mcp", resourceId: "*", tool: "search" }],
        },
        {
          scope: "mcp:blocked_write",
          selectors: [{ resourceKind: "mcp", resourceId: "server-b" }],
        },
        { scope: "org:read" },
      ],
    });
    fireEvent.change(screen.getByDisplayValue("Original description"), {
      target: { value: "New description" },
    });
    const form = savedForm();
    expect(form.addGrants ?? []).toEqual([]);
    expect(form.removeGrants ?? []).toEqual([]);
  });

  it("shows a block on a project on each of its servers", () => {
    show({
      ...role,
      grants: [
        {
          scope: "mcp:blocked_connect",
          selectors: [
            { resourceKind: "mcp", resourceId: "*", projectId: "project-a" },
          ],
        },
      ],
    });
    expect(
      screen
        .getByRole("button", { name: "Connect GitHub, blocked" })
        .getAttribute("aria-disabled"),
    ).toBe("true");
  });

  it("keeps a server's own grant clickable under a blocked project", () => {
    show({
      ...role,
      grants: [
        {
          scope: "mcp:blocked_connect",
          selectors: [
            { resourceKind: "mcp", resourceId: "*", projectId: "project-a" },
          ],
        },
        {
          scope: "mcp:connect",
          selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
        },
      ],
    });
    const use = screen.getByRole("button", { name: "Connect GitHub" });
    expect(use.getAttribute("aria-pressed")).toBe("true");
    expect(use.getAttribute("aria-disabled")).toBe("false");
  });

  it("marks what a project row passes down to its servers", () => {
    show({
      ...role,
      grants: [
        {
          scope: "mcp:read",
          selectors: [
            { resourceKind: "mcp", resourceId: "*", projectId: "project-a" },
          ],
        },
      ],
    });
    const use = screen.getByRole("button", { name: "Connect GitHub" });
    expect(use.getAttribute("aria-pressed")).toBe("false");
    expect(use.getAttribute("aria-disabled")).toBe("true");
    expect(
      screen
        .getByRole("button", { name: "View Default" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
  });
});

it("names the inference project selector for its permission, not View", () => {
  renderEditor();
  openPlatformTab();
  fireEvent.click(screen.getByRole("button", { name: "Run inference" }));
  expect(
    screen.queryByRole("button", { name: "Change where View applies" }),
  ).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "Change where Run inference applies" }),
  );
  expect(screen.getByRole("button", { name: "Select project A" })).toBeTruthy();
});

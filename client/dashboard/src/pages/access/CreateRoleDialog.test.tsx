import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";

import { CreateRoleDialog } from "./CreateRoleDialog";
import type { Role } from "@gram/client/models/components/role.js";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  refetchScopes: vi.fn(),
  scopesState: "ready" as "ready" | "loading" | "error",
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
vi.mock("@gram/client/react-query/agents.js", () => ({
  queryKeyAgents: () => ["agents"],
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { list: mocks.list } }),
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
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
    isLoading: mocks.scopesState === "loading",
    isError: mocks.scopesState === "error",
    refetch: mocks.refetchScopes,
    data:
      mocks.scopesState !== "ready"
        ? undefined
        : {
            scopes: [
              {
                slug: "org:read",
                resourceType: "org",
                category: "org",
                visibility: "user_visible",
                label: "Read organization",
              },
              {
                slug: "project:read",
                resourceType: "project",
                category: "project",
                visibility: "user_visible",
                description: "Read projects",
                exclusionScope: "project:blocked_read",
              },
              {
                slug: "inference:execute",
                resourceType: "inference",
                category: "inference",
                visibility: "user_visible",
                label: "Run inference",
                description: "Call the inference API in the selected projects.",
              },
              {
                slug: "log:read",
                resourceType: "project",
                category: "log",
                visibility: "user_visible",
                label: "View tool logs",
                description: "Read tool arguments and results.",
                sensitive: true,
                subScopes: [],
              },
              ...(["connect", "read", "write"] as const).map((action) => ({
                slug: `mcp:${action}`,
                resourceType: "mcp",
                category: "mcp",
                visibility: "user_visible",
                exclusionScope: `mcp:blocked_${action}`,
                // As the server lists them: Manage includes View, and
                // neither includes Connect.
                subScopes: action === "write" ? ["mcp:read"] : [],
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
vi.mock("./useServerTools", () => ({
  useServerTools: () => ({
    status: "ready",
    tools: [
      { id: "tool-a", name: "list_issues", type: "http" },
      { id: "tool-b", name: "delete_branch", type: "http" },
    ],
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
function renderEditor(
  editingRole?: Role,
  confirmAssignmentFor?: string,
  extra: Partial<React.ComponentProps<typeof CreateRoleDialog>> = {},
) {
  const client = new QueryClient();
  const tree = (role?: Role) => (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <CreateRoleDialog
            open
            onOpenChange={vi.fn<(open: boolean) => void>()}
            editingRole={role}
            confirmAssignmentFor={confirmAssignmentFor}
            presentation="page"
            {...extra}
          />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  const result = render(tree(editingRole));
  return {
    client,
    ...result,
    /** The roles list refreshed: the same editor, handed the role as stored
     *  now. */
    reread: (next: Role) => result.rerender(tree(next)),
  };
}
// Radix menus open on pointer down, not click.
function openMenu(name: string) {
  fireEvent.pointerDown(screen.getByRole("button", { name }), {
    button: 0,
    ctrlKey: false,
  });
}
/** Pick a platform area's level from its card. */
function setLevel(area: string, level: "No access" | "View" | "Manage") {
  openMenu(`${area} access level`);
  fireEvent.click(screen.getByRole("menuitemradio", { name: level }));
}
type SubmitLabel = "Save Changes" | "Create Role";
function reviewButtonName(label: SubmitLabel) {
  return label === "Save Changes" ? "Save role" : "Create role";
}
/** Save or create a role whose save adds a permission: the review step must
 *  open, and the save goes through from there. */
function submitReviewed(label: SubmitLabel) {
  fireEvent.click(screen.getByRole("button", { name: label }));
  fireEvent.click(
    screen.getByRole("button", { name: reviewButtonName(label) }),
  );
}
/** Save or create a role whose save adds nothing to review: the save goes
 *  through at once, and no review step opens. */
function submitDirect(label: SubmitLabel) {
  fireEvent.click(screen.getByRole("button", { name: label }));
  expect(
    screen.queryByRole("button", { name: reviewButtonName(label) }),
  ).toBeNull();
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.scopesState = "ready";
  mocks.scimEnabled = false;
  mocks.members = [];
  // agents.list is paginated: it resolves to pages, which the dialog drains.
  mocks.list.mockResolvedValue([{ result: { items: [] } }]);
  mocks.mappings = { groups: [], attributes: [], mappings: [] };
});
it("splits permissions into MCP access and platform access on one page", () => {
  renderEditor();
  expect(screen.getByRole("heading", { name: "MCP access" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "Platform access" })).toBeTruthy();
  expect(screen.queryByRole("tab")).toBeNull();
});

describe("starting a new role from a template", () => {
  it("fills the editor from the chosen starting point", () => {
    renderEditor();
    fireEvent.click(screen.getByRole("button", { name: /^Team reviewer/ }));
    expect(
      screen.getByRole("button", { name: "Tool logs access level" })
        .textContent,
    ).toContain("View");
    fireEvent.click(screen.getByRole("button", { name: "Create Role" }));
    // Tool logs are sensitive, so the review asks for a confirmation.
    const review = within(
      screen.getByRole("dialog", {
        name: "Review changes to “Team reviewer”",
      }),
    );
    fireEvent.click(review.getByRole("checkbox"));
    fireEvent.click(review.getByRole("button", { name: "Create role" }));
    expect(mocks.create.mock.calls[0]![0].request.createRoleForm).toMatchObject(
      {
        name: "Team reviewer",
        grants: expect.arrayContaining([
          { scope: "org:read" },
          { scope: "project:read" },
          { scope: "log:read" },
        ]),
      },
    );
  });

  it("stops matching the template once a permission changes", () => {
    renderEditor();
    const blank = screen.getByRole("button", { name: /^Blank/ });
    expect(blank.getAttribute("aria-pressed")).toBe("true");
    setLevel("Organization", "View");
    expect(blank.getAttribute("aria-pressed")).toBe("false");
  });

  it("asks before a template replaces permissions set by hand", () => {
    renderEditor();
    setLevel("Organization", "View");
    fireEvent.click(screen.getByRole("button", { name: /^Team reviewer/ }));
    // Nothing is replaced until the person confirms.
    fireEvent.click(
      within(
        screen.getByRole("dialog", {
          name: "Replace this role's permissions?",
        }),
      ).getByRole("button", { name: "Replace permissions" }),
    );
    expect(
      screen.getByRole("button", { name: "Tool logs access level" })
        .textContent,
    ).toContain("View");
  });

  it("offers no starting points for a role that already exists", () => {
    renderEditor(role);
    expect(screen.queryByText("Create from")).toBeNull();
  });
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
    setLevel("Organization", "View");

    const confirmation = confirmAssignment();
    setLevel("Organization", "No access");

    expect(confirmation.getAttribute("data-state")).toBe("unchecked");
  });

  it("clears an acknowledgement when a rule-editor change is saved", () => {
    renderEditor(undefined, "Denied User");
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    setLevel("Projects", "View");

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
    setLevel("Organization", "View");
    submitReviewed("Create Role");
    expect(mocks.create).toHaveBeenCalledOnce();
    expect(
      mocks.create.mock.calls[0]![0].request.createRoleForm,
    ).not.toHaveProperty("agentIds");
  });
});

describe("while the permission catalogue is unavailable", () => {
  // The review step reads sensitivity off the catalogue, so a save sent
  // before it loads could add a sensitive permission unreviewed.
  function createButton(): HTMLButtonElement {
    return screen.getByRole("button", {
      name: "Create Role",
    }) as HTMLButtonElement;
  }

  it("keeps the save disabled while the catalogue loads", () => {
    mocks.scopesState = "loading";
    renderEditor();
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    expect(createButton().disabled).toBe(true);
    fireEvent.click(createButton());
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("says why the save is disabled after the catalogue failed to load", () => {
    mocks.scopesState = "error";
    renderEditor();
    fireEvent.change(screen.getByPlaceholderText("e.g., Project Manager"), {
      target: { value: "Reader" },
    });
    expect(createButton().disabled).toBe(true);
    expect(
      screen.getByText(
        "Failed to load permissions, so the role can't be saved yet.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetchScopes).toHaveBeenCalledOnce();
  });
});

describe("a role pre-filled from an access request", () => {
  function createdGrants() {
    submitReviewed("Create Role");
    expect(mocks.create).toHaveBeenCalledOnce();
    return mocks.create.mock.calls[0]![0].request.createRoleForm.grants;
  }

  it("limits the permission to the requested server", () => {
    renderEditor(undefined, undefined, {
      defaultName: "Connect to GitHub",
      defaultScopes: ["mcp:connect"],
      defaultResource: { resourceId: "server-a", projectId: "project-a" },
    });
    expect(createdGrants()).toEqual([
      {
        scope: "mcp:connect",
        selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
      },
    ]);
  });

  it("limits a project permission to the requested project", () => {
    renderEditor(undefined, undefined, {
      defaultName: "Read the project",
      defaultScopes: ["project:read"],
      defaultResource: { projectId: "project-a" },
    });
    expect(createdGrants()).toEqual([
      {
        scope: "project:read",
        selectors: [{ resourceKind: "project", resourceId: "project-a" }],
      },
    ]);
  });

  it("stays unrestricted when no resource was requested", () => {
    renderEditor(undefined, undefined, {
      defaultName: "View organization",
      defaultScopes: ["org:read"],
      defaultResource: { resourceId: "org-a" },
    });
    expect(createdGrants()).toEqual([{ scope: "org:read" }]);
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

describe("the review step", () => {
  it("lists what a save adds and requires confirmation for sensitive access", () => {
    renderEditor(role);
    setLevel("Tool logs", "View");
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(mocks.update).not.toHaveBeenCalled();

    const review = within(
      screen.getByRole("dialog", { name: "Review changes to “Test role”" }),
    );
    expect(review.getByText("View tool logs")).toBeTruthy();
    expect(review.getByText("Sensitive")).toBeTruthy();
    expect(review.getByText("Everywhere")).toBeTruthy();
    expect(
      review.getByText("0 members hold this role today.", { exact: false }),
    ).toBeTruthy();
    const save = review.getByRole("button", { name: "Save role" });
    expect(save.getAttribute("disabled")).not.toBeNull();

    fireEvent.click(
      review.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    fireEvent.click(save);
    expect(mocks.update).toHaveBeenCalledOnce();
    expect(
      mocks.update.mock.calls[0]![0].request.updateRoleForm.addGrants,
    ).toEqual([{ scope: "log:read", selectors: undefined }]);
  });

  it("needs no confirmation when nothing added is sensitive", () => {
    renderEditor(role);
    setLevel("Organization", "View");
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    const review = within(
      screen.getByRole("dialog", { name: "Review changes to “Test role”" }),
    );
    expect(review.queryByRole("checkbox")).toBeNull();
    const save = review.getByRole("button", { name: "Save role" });
    expect(save.getAttribute("disabled")).toBeNull();
    fireEvent.click(save);
    expect(mocks.update).toHaveBeenCalledOnce();
  });

  it("goes back to the form without saving", () => {
    renderEditor(role);
    setLevel("Organization", "View");
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.queryByRole("button", { name: "Save role" })).toBeNull();
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("skips the step when the save adds no permission", () => {
    renderEditor(role);
    fireEvent.change(
      screen.getByPlaceholderText("Describe what this role can do..."),
      { target: { value: "Updated description" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(screen.queryByRole("button", { name: "Save role" })).toBeNull();
    expect(mocks.update).toHaveBeenCalledOnce();
  });

  function addAda() {
    mocks.members = [
      {
        id: "member-1",
        name: "Ada Lovelace",
        email: "ada@example.com",
        roleIds: [],
      },
    ];
  }
  function tickAda() {
    fireEvent.click(screen.getByText("Assign Members"));
    fireEvent.click(screen.getByText("Ada Lovelace"));
  }

  it("reviews the sensitive access new holders gain from a role that already grants it", () => {
    // The grant diff is empty, but the member gains log:read by holding the
    // role, which is what the audit row records; the review says so.
    addAda();
    renderEditor({ ...role, grants: [{ scope: "log:read" }] });
    tickAda();
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(mocks.update).not.toHaveBeenCalled();

    const review = within(
      screen.getByRole("dialog", { name: "Review changes to “Test role”" }),
    );
    expect(
      review.getByText("Sensitive access for 1 new member · 1"),
    ).toBeTruthy();
    expect(review.getByText("View tool logs")).toBeTruthy();
    expect(review.queryByText("Added permission", { exact: false })).toBeNull();
    const save = review.getByRole("button", { name: "Save role" });
    expect(save.getAttribute("disabled")).not.toBeNull();

    fireEvent.click(
      review.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    fireEvent.click(save);
    expect(mocks.update).toHaveBeenCalledOnce();
    const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
    expect(form.memberIds).toEqual(["member-1"]);
    expect(form.addGrants ?? []).toEqual([]);
  });

  it("closes the review when the role is read again with other grants, and saves what the next review shows", () => {
    // The roles list keeps being refreshed while the editor is open. A review
    // opened before the role gained a grant no longer describes the save, so
    // it closes rather than commit grants nobody reviewed; the next save is
    // reviewed afresh and keeps the grant the role gained.
    const { reread } = renderEditor(role);
    setLevel("Tool logs", "View");
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(screen.getByRole("button", { name: "Save role" })).toBeTruthy();

    const linear = [{ resourceKind: "mcp" as const, resourceId: "server-b" }];
    reread({ ...role, grants: [{ scope: "mcp:connect", selectors: linear }] });
    expect(screen.queryByRole("button", { name: "Save role" })).toBeNull();
    expect(mocks.update).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    const review = within(
      screen.getByRole("dialog", { name: "Review changes to “Test role”" }),
    );
    expect(review.getByText("View tool logs")).toBeTruthy();
    fireEvent.click(
      review.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    fireEvent.click(review.getByRole("button", { name: "Save role" }));
    expect(mocks.update).toHaveBeenCalledOnce();
    const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
    expect(form.addGrants).toEqual([
      { scope: "log:read", selectors: undefined },
    ]);
    expect(form.removeGrants ?? []).toEqual([]);
  });

  it("saves straight away when new holders gain nothing sensitive", () => {
    addAda();
    renderEditor({ ...role, grants: [{ scope: "org:read" }] });
    tickAda();
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
    expect(screen.queryByRole("button", { name: "Save role" })).toBeNull();
    expect(mocks.update).toHaveBeenCalledOnce();
    expect(
      mocks.update.mock.calls[0]![0].request.updateRoleForm.memberIds,
    ).toEqual(["member-1"]);
  });
});

describe("the access grid", () => {
  /** The default project, which the servers sit in, starts open. */
  function show(editingRole: Parameters<typeof renderEditor>[0]) {
    renderEditor(editingRole);
  }

  function savedForm(how: "reviewed" | "direct") {
    (how === "reviewed" ? submitReviewed : submitDirect)("Save Changes");
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
    expect(savedForm("reviewed").addGrants).toEqual([
      {
        scope: "mcp:write",
        selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
      },
    ]);
  });

  it("keeps Connect ticked when All tools is unticked", () => {
    show({
      ...role,
      grants: [
        {
          scope: "mcp:connect",
          selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
        },
      ],
    });
    const allTools = () =>
      screen.getByRole("button", { name: "All tools on GitHub" });
    expect(allTools().getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(allTools());
    expect(allTools().getAttribute("aria-pressed")).toBe("false");
    expect(
      screen
        .getByRole("button", { name: "Connect GitHub" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    expect(screen.getByText("No tools")).toBeTruthy();
    // The server's tools unfold, ready to be picked one by one.
    fireEvent.click(
      screen.getByRole("button", { name: "Connect list_issues on GitHub" }),
    );
    expect(screen.getByText("1 tool")).toBeTruthy();
    // Unticking Connect itself still takes it away.
    fireEvent.click(screen.getByRole("button", { name: "Connect GitHub" }));
    expect(
      screen
        .getByRole("button", { name: "Connect GitHub" })
        .getAttribute("aria-pressed"),
    ).toBe("false");
  });

  it("ticks every server and tool under a project, and lets one server be left out", () => {
    show(role);
    const pressed = (name: string) =>
      screen.getByRole("button", { name }).getAttribute("aria-pressed");
    fireEvent.click(screen.getByRole("button", { name: "Connect Default" }));
    // The project reaches every tool, and so does each server in it.
    expect(pressed("Connect Default")).toBe("true");
    expect(pressed("All tools on Default")).toBe("true");
    expect(pressed("Connect GitHub")).toBe("true");
    expect(pressed("All tools on GitHub")).toBe("true");
    // One server can be left out: the others keep Connect.
    fireEvent.click(screen.getByRole("button", { name: "Connect GitHub" }));
    expect(pressed("Connect GitHub")).toBe("false");
    expect(pressed("Connect Linear")).toBe("true");
    expect(pressed("All tools on Linear")).toBe("true");
    expect(savedForm("reviewed").addGrants).toEqual([
      {
        scope: "mcp:connect",
        selectors: [{ resourceKind: "mcp", resourceId: "server-b" }],
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

  it("denies a server from its row menu, once acknowledged", () => {
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
    openMenu("More for Linear");
    fireEvent.click(screen.getByRole("menuitem", { name: /Deny explicitly/ }));
    const dialog = within(
      screen.getByRole("dialog", { name: "Deny MCP access for this role" }),
    );
    const add = dialog.getByRole("button", { name: "Add deny" });
    expect(add.hasAttribute("disabled")).toBe(true);
    fireEvent.click(dialog.getByRole("checkbox"));
    fireEvent.click(add);
    expect(
      within(screen.getByRole("region", { name: "Explicit denies" })).getByText(
        "MCP access",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("1 explicit deny · overrides every other grant"),
    ).toBeTruthy();
    const form = savedForm("direct");
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
    const form = savedForm("direct");
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

  it("lets a role that can manage a server be granted Connect on it", () => {
    show({
      ...role,
      grants: [
        {
          scope: "mcp:write",
          selectors: [{ resourceKind: "mcp", resourceId: "server-a" }],
        },
      ],
    });
    // Manage includes View, but connecting is a separate permission.
    expect(
      screen
        .getByRole("button", { name: "View GitHub" })
        .getAttribute("aria-disabled"),
    ).toBe("true");
    const use = screen.getByRole("button", { name: "Connect GitHub" });
    expect(use.getAttribute("aria-pressed")).toBe("false");
    expect(use.getAttribute("aria-disabled")).toBe("false");
    // Neither does it reach the server's tools.
    expect(
      screen
        .getByRole("button", { name: "All tools on GitHub" })
        .getAttribute("aria-pressed"),
    ).toBe("false");
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
    // View passes down to the project's servers, where it can be unticked.
    const view = screen.getByRole("button", { name: "View GitHub" });
    expect(view.getAttribute("aria-pressed")).toBe("true");
    expect(view.getAttribute("aria-disabled")).toBe("false");
    // View does not include Connect, so Connect stays off and can be granted.
    const use = screen.getByRole("button", { name: "Connect GitHub" });
    expect(use.getAttribute("aria-pressed")).toBe("false");
    expect(use.getAttribute("aria-disabled")).toBe("false");
    expect(
      screen
        .getByRole("button", { name: "View Default" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
  });

  it("unticks a level one server gets from a wider grant by splitting it", () => {
    show({
      ...role,
      grants: [{ scope: "mcp:connect" }],
    });
    // Connect on every server shows on each server, and can be unticked.
    fireEvent.click(screen.getByRole("button", { name: "Connect GitHub" }));
    expect(
      screen
        .getByRole("button", { name: "Connect GitHub" })
        .getAttribute("aria-pressed"),
    ).toBe("false");
    expect(
      screen
        .getByRole("button", { name: "Connect Linear" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    const form = savedForm("reviewed");
    expect(form.removeGrants).toEqual([{ scope: "mcp:connect" }]);
    expect(form.addGrants).toEqual([
      {
        scope: "mcp:connect",
        selectors: [{ resourceKind: "mcp", resourceId: "server-b" }],
      },
    ]);
  });
});

it("names the inference project selector for its permission, not View", () => {
  renderEditor();
  fireEvent.click(screen.getByRole("checkbox", { name: "Run inference" }));
  expect(
    screen.queryByRole("button", { name: "Change where View applies" }),
  ).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "Change where Run inference applies" }),
  );
  expect(screen.getByRole("button", { name: "Select project A" })).toBeTruthy();
});

describe("a role read again while the editor is open", () => {
  type Grant = Role["grants"][number];
  const personalAuth: Grant = {
    scope: "mcp:auth",
    selectors: [
      {
        resourceKind: "mcp_auth_client",
        resourceId: "*",
        clientType: "remote_session",
        connectionOwner: "$represented_user",
      },
    ],
  };
  const blockedAuth: Grant = {
    scope: "mcp:blocked_auth",
    selectors: [
      {
        resourceKind: "mcp_auth_client",
        resourceId: "*",
        clientType: "service_account",
      },
    ],
  };
  const linear: Grant["selectors"] = [
    { resourceKind: "mcp", resourceId: "server-b" },
  ];

  /** One credential type's control, named by the credential it decides. */
  function credential(label: string) {
    return within(
      screen.getByRole("group", { name: `${label} for this role` }),
    );
  }
  function choose(label: string, option: string) {
    fireEvent.click(credential(label).getByRole("button", { name: option }));
  }
  function chosen(label: string): string[] {
    return credential(label)
      .getAllByRole("button")
      .filter((button) => button.getAttribute("aria-pressed") === "true")
      .map((button) => button.textContent ?? "");
  }
  function rename() {
    fireEvent.change(screen.getByDisplayValue("Test role"), {
      target: { value: "Renamed role" },
    });
  }
  function savedForm(how: "reviewed" | "direct") {
    (how === "reviewed" ? submitReviewed : submitDirect)("Save Changes");
    expect(mocks.update).toHaveBeenCalledOnce();
    return mocks.update.mock.calls[0]![0].request.updateRoleForm;
  }

  it("does not revoke a credential rule the role gained while the editor was open", () => {
    // The editor filled in from a read with no credential rules, and the role
    // gained one before the person renamed it. A save works out what to take
    // off the role from what the form holds, so renaming must not revoke a
    // credential nobody here touched.
    const { reread } = renderEditor(role);
    reread({ ...role, grants: [personalAuth] });

    rename();
    const form = savedForm("direct");
    expect(form.name).toBe("Renamed role");
    expect(form.removeGrants ?? []).toEqual([]);
    expect(form.addGrants ?? []).toEqual([]);
  });

  it("shows a credential rule the role gained while the editor was open", () => {
    const { reread } = renderEditor(role);
    reread({ ...role, grants: [personalAuth] });

    expect(chosen("Each person's own account")).toEqual(["Can use"]);
  });

  it("keeps a credential rule the editor started with", () => {
    renderEditor({ ...role, grants: [personalAuth] });
    rename();
    const form = savedForm("direct");
    expect(form.removeGrants ?? []).toEqual([]);
    expect(form.addGrants ?? []).toEqual([]);
  });

  it("does not undo a block the role gained while the editor was open", () => {
    const { reread } = renderEditor({ ...role, grants: [personalAuth] });
    reread({ ...role, grants: [personalAuth, blockedAuth] });

    rename();
    const form = savedForm("direct");
    expect(form.removeGrants ?? []).toEqual([]);
    expect(form.addGrants ?? []).toEqual([]);
  });

  it("shows a block the role gained while the editor was open", () => {
    const { reread } = renderEditor({ ...role, grants: [personalAuth] });
    reread({ ...role, grants: [personalAuth, blockedAuth] });

    expect(chosen("Service account")).toEqual(["Blocked"]);
  });

  it("does not revoke a permission the role gained while the editor was open", () => {
    const { reread } = renderEditor(role);
    reread({ ...role, grants: [{ scope: "mcp:connect", selectors: linear }] });

    rename();
    const form = savedForm("direct");
    expect(form.removeGrants ?? []).toEqual([]);
    expect(form.addGrants ?? []).toEqual([]);
  });

  it("shows a permission the role gained while the editor was open", () => {
    const { reread } = renderEditor(role);
    reread({ ...role, grants: [{ scope: "mcp:connect", selectors: linear }] });

    expect(
      screen
        .getByRole("button", { name: "Connect Linear" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
  });

  it("keeps the credential the person chose and the rule the role gained", () => {
    const { reread } = renderEditor(role);
    choose("Each person's own account", "Can use");
    reread({ ...role, grants: [blockedAuth] });

    const form = savedForm("direct");
    expect(form.addGrants).toEqual([personalAuth]);
    expect(form.removeGrants ?? []).toEqual([]);
    expect(chosen("Each person's own account")).toEqual(["Can use"]);
    expect(chosen("Service account")).toEqual(["Blocked"]);
  });

  it("takes away the stored rule the person switched off", () => {
    // Turning a credential off is an explicit edit, and its removal has to
    // name the selector the role stores so the server matches it.
    renderEditor({ ...role, grants: [personalAuth] });
    choose("Each person's own account", "Not set");
    const form = savedForm("direct");
    expect(form.removeGrants).toEqual([personalAuth]);
    expect(form.addGrants ?? []).toEqual([]);
  });

  it("takes away the permission the person switched off, not the one the role gained", () => {
    const github: Grant["selectors"] = [
      { resourceKind: "mcp", resourceId: "server-a" },
    ];
    const { reread } = renderEditor({
      ...role,
      grants: [{ scope: "mcp:connect", selectors: github }],
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect GitHub" }));
    reread({
      ...role,
      grants: [
        { scope: "mcp:connect", selectors: github },
        { scope: "mcp:read", selectors: linear },
      ],
    });

    const form = savedForm("direct");
    expect(form.removeGrants).toEqual([
      { scope: "mcp:connect", selectors: github },
    ]);
    expect(form.addGrants ?? []).toEqual([]);
  });

  it("leaves the name being typed alone and offers nothing to save for the read itself", () => {
    const { reread } = renderEditor(role);
    rename();
    reread({ ...role, grants: [personalAuth] });

    expect(screen.getByDisplayValue("Renamed role")).toBeTruthy();
    fireEvent.change(screen.getByDisplayValue("Renamed role"), {
      target: { value: "Test role" },
    });
    expect(
      screen
        .getByRole("button", { name: "Save Changes" })
        .getAttribute("disabled"),
    ).not.toBeNull();
  });
});

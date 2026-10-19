import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const state = vi.hoisted(() => ({
  audience: vi.fn(),
  inventory: vi.fn(),
  identity: vi.fn(),
  explain: vi.fn(),
  mutate: vi.fn(),
  manyRoles: false,
}));
const role = {
  id: "engineer",
  name: "Engineering",
  principalUrn: "role:organization:engineer",
  memberCount: 1,
  agentIds: [],
};
const member = {
  id: "lee",
  name: "Lee",
  email: "lee@example.test",
  roleIds: ["engineer"],
  directoryRoleIds: [],
  principalUrn: "user:lee",
};
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org",
    projects: [{ id: "project", slug: "default" }],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    mcpServers: { listForOrg: state.inventory },
    access: {
      listResourceAudience: state.audience,
      listIdentityAccess: state.identity,
      explainResourceAccess: state.explain,
    },
  }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: { members: [member] },
    isSuccess: true,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({
    data: {
      roles: [
        ...(state.manyRoles
          ? Array.from({ length: 48 }, (_, i) => ({
              ...role,
              id: `role-${i}`,
              name: `Role ${i}`,
              principalUrn: role.principalUrn,
            }))
          : []),
        role,
        {
          ...role,
          id: "empty",
          name: "Unassigned",
          principalUrn: "role:organization:empty",
        },
      ],
    },
    isSuccess: true,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/setResourceAudience.js", () => ({
  useSetResourceAudienceMutation: () => ({ mutate: state.mutate }),
}));
vi.mock("@/components/member-facepile", () => ({
  MemberFacepile: ({
    members,
    onMemberSelect,
  }: {
    members: { id: string; name: string }[];
    onMemberSelect?: (member: { id: string; name: string }) => void;
  }) =>
    members.length ? (
      <button
        onClick={(event) => {
          event.stopPropagation();
          onMemberSelect?.(members[0]!);
        }}
      >
        Pick {members[0]!.name}
      </button>
    ) : null,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ access: { roles: { href: () => "/access/roles" } } }),
  useRoutes: () => ({
    mcp: { x: { overview: { href: (slug: string) => `/servers/${slug}` } } },
  }),
}));
vi.mock("./PersonServerAssignment", () => ({
  ServerAssignment: ({ server }: { server: { name: string } }) => (
    <div>{server.name}</div>
  ),
}));
vi.mock("./AssignmentServerIcon", () => ({
  AssignmentServerIcon: ({ server }: { server: { name: string } }) => (
    <span data-testid="server-icon">{server.name}</span>
  ),
}));
import { MCPAssignments } from "./MCPAssignments";
function mount(onReady: (ready: boolean) => void = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <MemoryRouter>
          <MCPAssignments onReady={onReady} />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { client, onReady };
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.manyRoles = false;
  state.inventory.mockResolvedValue({
    mcpServers: Array.from({ length: 50 }, (_, i) => ({
      id: `server-${i}`,
      name: `Server ${String(i).padStart(2, "0")}`,
      slug: `server-${i}`,
      projectId: "project",
      visibility: "private",
    })),
  });
  state.audience.mockResolvedValue({
    version: "v1",
    entries: [
      {
        principalUrn: role.principalUrn,
        kind: "role",
        displayName: role.name,
        level: "use",
        appliesTo: "resource",
        dispositions: ["read_only"],
        memberIds: [member.id],
      },
    ],
  });
  state.identity.mockResolvedValue({
    servers: [
      { id: "allowed", name: "Allowed server" },
      { id: "denied", name: "Denied server" },
    ],
  });
  state.explain.mockImplementation(
    async ({ resourceId }: { resourceId: string }) => ({
      levels: [{ level: "use", allowed: resourceId === "allowed", rules: [] }],
    }),
  );
});
it("shows compact icon stacks at 50 servers and expands ten at a time", async () => {
  const { onReady } = mount();
  await screen.findByText("Engineering");
  expect(screen.queryAllByRole("link")).toHaveLength(0);
  expect(screen.getAllByTestId("server-icon")).toHaveLength(8);
  expect(screen.getByText("+42 more")).toBeTruthy();
  expect(screen.getByText("Unassigned")).toBeTruthy();
  expect(state.audience).toHaveBeenCalledTimes(50);
  expect(state.identity).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText("Engineering"));
  expect(screen.getAllByRole("link")).toHaveLength(10);
  fireEvent.click(screen.getByRole("button", { name: "Show 10 more" }));
  expect(screen.getAllByRole("link")).toHaveLength(20);
  await waitFor(() => expect(onReady).toHaveBeenLastCalledWith(true));
});
it("fails a complete review when one server cannot be loaded", async () => {
  state.audience.mockRejectedValue(new Error("Forbidden"));
  const { onReady } = mount();
  await screen.findByRole("alert");
  expect(onReady).not.toHaveBeenCalledWith(true);
  expect(screen.queryByText("Engineering")).toBeNull();
});
it("resolves only the selected user and excludes denied connection access", async () => {
  mount();
  await screen.findByText("Engineering");
  fireEvent.click(screen.getByRole("button", { name: "Check user access" }));
  fireEvent.change(
    screen.getByPlaceholderText("Find a person by name or email…"),
    { target: { value: "Lee" } },
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Lee · lee@example.test" }),
  );
  await screen.findByText("Allowed server");
  expect(screen.queryByText("Denied server")).toBeNull();
  expect(state.identity).toHaveBeenCalledTimes(1);
});
it("confirms a precise role block against a fresh audience version", async () => {
  mount();
  await screen.findByText("Engineering");
  fireEvent.click(screen.getByText("Engineering"));
  await userEvent.click(
    screen.getByRole("button", {
      name: "Access actions for Engineering on Server 00",
    }),
  );
  await userEvent.click(
    await screen.findByRole("menuitem", { name: /Block access/ }),
  );
  await screen.findByRole("heading", {
    name: "Block Engineering from Server 00?",
  });
  const confirm = screen
    .getAllByRole("button", { name: "Block access" })
    .at(-1)!;
  await waitFor(() => expect(confirm.hasAttribute("disabled")).toBe(false));
  fireEvent.click(confirm);
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      setResourceAudienceForm: {
        resourceKind: "mcp",
        resourceId: "server-0",
        expectedVersion: "v1",
        entries: expect.arrayContaining([
          expect.objectContaining({
            principalUrn: role.principalUrn,
            level: "blocked",
          }),
        ]),
      },
    },
  });
});

it("keeps a 50-role overview bounded without repeating server requests", async () => {
  state.manyRoles = true;
  mount();
  await screen.findByText("50 roles");
  await screen.findByText("Engineering");
  expect(screen.queryAllByRole("link")).toHaveLength(0);
  expect(screen.getAllByTestId("server-icon")).toHaveLength(49 * 8);
  expect(state.audience).toHaveBeenCalledTimes(50);
  expect(state.identity).not.toHaveBeenCalled();
});

it("removes only this role’s grants without creating a block or removing other roles", async () => {
  const other = {
    principalUrn: "role:organization:other",
    kind: "role",
    displayName: "Other",
    level: "use",
    appliesTo: "resource",
  };
  state.audience.mockResolvedValue({
    version: "v2",
    entries: [
      {
        ...other,
        principalUrn: role.principalUrn,
        displayName: role.name,
        level: "manage",
      },
      other,
    ],
  });
  mount();
  await screen.findByText("Engineering");
  fireEvent.click(screen.getByText("Engineering"));
  expect(screen.getAllByText("All tools")).toHaveLength(10);
  expect(screen.queryByText("Allows all tools across all servers")).toBeNull();
  await userEvent.click(
    screen.getByRole("button", {
      name: "Access actions for Engineering on Server 00",
    }),
  );
  await userEvent.click(
    await screen.findByRole("menuitem", { name: /Remove access/ }),
  );
  const confirm = await screen.findByRole("button", { name: "Remove access" });
  await waitFor(() => expect(confirm.hasAttribute("disabled")).toBe(false));
  fireEvent.click(confirm);
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      setResourceAudienceForm: {
        resourceKind: "mcp",
        resourceId: "server-0",
        expectedVersion: "v2",
        entries: [
          {
            principalUrn: other.principalUrn,
            level: "use",
            tools: undefined,
            dispositions: undefined,
          },
        ],
      },
    },
  });
});

it("hides server search for fewer than ten servers", async () => {
  state.inventory.mockResolvedValue({
    mcpServers: Array.from({ length: 9 }, (_, i) => ({
      id: `server-${i}`,
      name: `Server ${i}`,
      slug: `server-${i}`,
      projectId: "project",
      visibility: "private",
    })),
  });
  mount();
  fireEvent.click(await screen.findByText("Engineering"));
  expect(screen.queryByPlaceholderText("Search servers…")).toBeNull();
});

it("opens the MCP selector directly without a filter drawer", async () => {
  mount();
  await screen.findByText("Engineering");
  await userEvent.click(
    screen.getByRole("button", { name: /MCP server filter:/ }),
  );
  expect(await screen.findByPlaceholderText("Search mcp server…")).toBeTruthy();
  await userEvent.click(screen.getByRole("option", { name: "Server 00" }));
  expect(screen.queryByText("Unassigned")).toBeNull();
  fireEvent.click(screen.getByText("Engineering"));
  expect(screen.getAllByRole("link")).toHaveLength(1);
});

it("shows blocked servers even when the role has no grant, and unblocks without granting access", async () => {
  state.audience.mockResolvedValue({
    version: "v3",
    entries: [
      {
        principalUrn: role.principalUrn,
        kind: "role",
        displayName: role.name,
        level: "blocked",
        appliesTo: "resource",
      },
    ],
  });
  mount();
  await screen.findByLabelText("Server 00: Blocked");
  expect(screen.getByLabelText("Server 00: Blocked")).toBeTruthy();
  fireEvent.click(screen.getByText("Engineering"));
  expect(screen.getAllByText("Blocked")).toHaveLength(10);
  expect(screen.queryByText("Default project")).toBeNull();
  await userEvent.click(
    screen.getByRole("button", {
      name: "Access actions for Engineering on Server 00",
    }),
  );
  await userEvent.click(
    await screen.findByRole("menuitem", { name: /Unblock access/ }),
  );
  const confirm = await screen.findByRole("button", { name: "Unblock access" });
  await waitFor(() => expect(confirm.hasAttribute("disabled")).toBe(false));
  fireEvent.click(confirm);
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      setResourceAudienceForm: {
        resourceKind: "mcp",
        resourceId: "server-0",
        expectedVersion: "v3",
        entries: [],
      },
    },
  });
});

it("lists allowed servers before blocked servers", async () => {
  state.inventory.mockResolvedValue({
    mcpServers: [
      {
        id: "blocked",
        name: "A blocked",
        slug: "blocked",
        projectId: "project",
        visibility: "private",
      },
      {
        id: "allowed",
        name: "Z allowed",
        slug: "allowed",
        projectId: "project",
        visibility: "private",
      },
    ],
  });
  state.audience.mockImplementation(
    async ({ resourceId }: { resourceId: string }) => ({
      version: "v1",
      entries: [
        {
          principalUrn: role.principalUrn,
          kind: "role",
          displayName: role.name,
          level: resourceId === "blocked" ? "blocked" : "use",
          appliesTo: "resource",
        },
      ],
    }),
  );
  mount();
  fireEvent.click(await screen.findByText("Engineering"));
  expect(screen.getAllByRole("link").map((link) => link.textContent)).toEqual([
    "Z allowed",
    "A blocked",
  ]);
});

it("opens effective access with a member selected from a role roster", async () => {
  mount();
  await screen.findByText("Engineering");
  expect(screen.queryByText("View members")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "Pick Lee" }));
  await screen.findByRole("heading", { name: "Check user access" });
  await screen.findByText("Allowed server");
  expect(state.identity).toHaveBeenCalledWith(
    { userId: "lee" },
    undefined,
    expect.anything(),
  );
  expect(
    screen.queryByPlaceholderText("Search MCPs in Engineering…"),
  ).toBeNull();
});

it("includes blocked servers that the identity access listing omits", async () => {
  state.audience.mockImplementation(
    async ({ resourceId }: { resourceId: string }) => ({
      version: "v1",
      entries: [
        {
          principalUrn: role.principalUrn,
          kind: "role",
          displayName: role.name,
          level: resourceId === "server-0" ? "blocked" : "use",
          appliesTo: "resource",
        },
      ],
    }),
  );
  state.explain.mockImplementation(
    async ({ resourceId }: { resourceId: string }) => ({
      visibility: "private",
      levels: [
        {
          level: "use",
          allowed: resourceId === "allowed",
          rules:
            resourceId === "server-0"
              ? [
                  {
                    effect: "blocks",
                    kind: "role",
                    displayName: "Engineering",
                    principalUrn: role.principalUrn,
                  },
                ]
              : [],
        },
      ],
    }),
  );
  mount();
  await userEvent.click(
    await screen.findByRole("button", { name: "Pick Lee" }),
  );
  await screen.findByText("Allowed server");
  expect(screen.getByText("Server 00")).toBeTruthy();
  expect(screen.queryByText("Denied server")).toBeNull();
  expect(state.explain).toHaveBeenCalledWith(
    expect.objectContaining({ resourceId: "server-0", userId: "lee" }),
    undefined,
    expect.anything(),
  );
});

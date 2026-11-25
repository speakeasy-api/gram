import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { AgentIdentityPermissions } from "./AgentIdentityAccess";
import { invalidateAgentPolicy } from "@/pages/agents/agent-policy-grants";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  listMembers: vi.fn(),
  getUpstream: vi.fn(),
  identityLink: vi.fn(),
  allAuthClients: vi.fn(),
  hasScope: vi.fn(),
  scopesError: false,
}));
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({
    identifier,
    children,
  }: {
    identifier: { userId: string };
    children: React.ReactNode;
  }) => {
    mocks.identityLink(identifier);
    return <a href="#identity">{children}</a>;
  },
}));
const authClients = [
  {
    id: "client_calendar",
    name: "Content calendar",
    clientType: "service_account",
    servers: [
      {
        id: "server_calendar",
        slug: "calendar",
        name: "Calendar server",
        projectId: "project_example",
      },
    ],
  },
  {
    id: "client_notes",
    name: "Meeting notes",
    clientType: "remote_session",
    remoteSessionClientId: "upstream_notes",
    servers: [
      {
        id: "server_notes",
        slug: "notes",
        name: "Notes server",
        projectId: "project_example",
      },
    ],
  },
];
// Like the real query, the list loads only when enabled.
vi.mock("@/lib/useAllAuthClients", () => ({
  useAllAuthClients: (options: { enabled: boolean }) =>
    mocks.allAuthClients(options),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope, isLoading: false, error: null }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "owner_example" } }),
  useOrganization: () => ({
    id: "org_example",
    projects: [
      { id: "project_example", name: "Example project", slug: "example" },
    ],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    agents: { listPolicyGrants: mocks.list },
    access: { listMembers: mocks.listMembers },
    organizationRemoteSessionClients: { get: mocks.getUpstream },
  }),
  useSlugs: () => ({ orgSlug: "org", projectSlug: "example" }),
}));
// Whom the agent acts for is read from its connections; here it only matters
// which rows ask.
vi.mock("./GrantOnBehalfOf", () => ({
  GrantOnBehalfOf: ({ resourceId }: { resourceId: string }) => (
    <span>{`on behalf of, for ${resourceId}`}</span>
  ),
}));
vi.mock("@gram/client/react-query/listAgentPolicyScopes.js", () => ({
  useListAgentPolicyScopes: () => ({
    data: mocks.scopesError
      ? undefined
      : {
          scopes: [
            {
              slug: "inference:execute",
              label: "Run inference",
              description: "Run inference in selected projects.",
              resourceType: "inference",
              category: "inference",
              visibility: "user_visible",
              agentEligible: true,
              sensitive: false,
              subScopes: [],
            },
          ],
        },
    isLoading: false,
    isError: mocks.scopesError,
  }),
}));
vi.mock("@/pages/access/useResourceLinkMaps", () => ({
  useResourceLinkMaps: () => ({
    projectMap: new Map([
      ["project_example", { slug: "example", name: "Example project" }],
      ["project_other", { slug: "other", name: "Other project" }],
    ]),
    toolsetMap: new Map([
      [
        "server_example",
        {
          slug: "example-server",
          name: "Example server",
          projectId: "project_example",
        },
      ],
    ]),
    mcpServerMap: new Map([
      [
        "server_remote",
        {
          slug: "remote-server",
          name: "Remote server",
          projectId: "project_example",
        },
      ],
    ]),
  }),
}));
const agent: ManagedAgent = {
  id: "agent_example",
  name: "Example agent",
  ownerUserId: "owner_example",
  lifecycle: "active",
  permissions: { read: true, write: true, authorize: true, transfer: true },
  createdAt: new Date(),
  updatedAt: new Date(),
};
function setup(subject = agent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AgentIdentityPermissions agent={subject} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.hasScope.mockReturnValue(true);
  mocks.allAuthClients.mockImplementation(({ enabled }) =>
    enabled
      ? { data: authClients, isSuccess: true }
      : { data: undefined, isSuccess: false },
  );
  mocks.scopesError = false;
  mocks.list.mockResolvedValue([]);
  mocks.listMembers.mockResolvedValue({
    members: [
      { id: "user_ada", name: "Ada Lovelace", email: "ada@example.com" },
    ],
  });
  mocks.getUpstream.mockImplementation(async ({ id }: { id: string }) => {
    if (id !== "upstream_notes") throw new Error("Not found");
    return {
      id,
      clientId: "notes-oauth-app",
      displayName: "Notes OAuth app",
      remoteSessionIssuerId: "issuer_notes",
    };
  });
});
describe("Agent profile permissions", () => {
  it("opens the shared permission editor without a separate inference card", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "inference_grant",
        scope: "inference:execute",
        selector: {
          resourceKind: "inference",
          resourceId: "*",
          projectId: "project_example",
        },
      },
    ]);
    setup();
    const slug = await screen.findByText("inference:execute");
    expect(slug.parentElement?.textContent).toBe(
      "Run inference (inference:execute)",
    );
    expect(
      screen.getByText("Run inference in selected projects."),
    ).toBeTruthy();
    expect(screen.getAllByText("Agent permissions")).toHaveLength(1);
    expect(screen.queryByText("Inference access")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Edit permissions" }));
    expect(
      screen.getByRole("dialog", { name: "Edit permissions" }),
    ).toBeTruthy();
    expect(screen.getByRole("tab", { name: /Platform access/ })).toBeTruthy();
  });
  it("shows configured permissions and every selector constraint", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "grant_example",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_example",
          projectId: "project_example",
          tool: "search",
          disposition: "read_only",
          serverIdentity: "example",
          serverUrl: "https://example.test/mcp",
        },
      },
    ]);
    setup();
    expect(await screen.findByText("mcp:connect")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /Example server/ }).getAttribute("href"),
    ).toBe("/org/projects/example/toolsets/example-server");
    expect(
      screen.getByText(
        /Project: Example project.*Tool: search.*Disposition: read_only.*Server identity: example.*Server URL: https:\/\/example.test\/mcp/,
      ),
    ).toBeTruthy();
    expect(screen.getByText("on behalf of, for server_example")).toBeTruthy();
    expect(mocks.list).toHaveBeenCalledWith({ agentId: agent.id }, undefined, {
      signal: expect.any(AbortSignal),
    });
  });
  it("lists the servers the agent reaches first, changed on its MCP sign-in tab", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "read_grant",
        scope: "project:read",
        selector: { resourceKind: "project", resourceId: "project_example" },
      },
      {
        id: "connect_grant",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_example",
          projectId: "project_example",
        },
      },
    ]);
    setup();
    await screen.findByText("mcp:connect");
    const titles = screen
      .getAllByText(/^(MCP servers|Other permissions)$/)
      .map((title) => title.textContent);
    expect(titles).toEqual(["MCP servers", "Other permissions"]);
    expect(
      screen.getByRole("link", { name: "Change servers" }).getAttribute("href"),
    ).toMatch(/\/identities\/agent%3Aagent_example\/mcp-sign-in$/);
    expect(screen.getAllByText("mcp:connect")).toHaveLength(1);
    expect(screen.getByText("project:read")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Edit permissions" }));
    const dialog = screen.getByRole("dialog", { name: "Edit permissions" });
    expect(dialog.textContent).not.toContain("mcp:connect");
  });
  it("offers to add servers when the agent reaches none", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "read_grant",
        scope: "project:read",
        selector: { resourceKind: "project", resourceId: "project_example" },
      },
    ]);
    setup();
    expect(
      await screen.findByText("This agent cannot connect to any MCP server."),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "Change servers" })).toBeTruthy();
  });
  it("does not say the agent cannot connect when mcp:write lets it", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "write_grant",
        scope: "mcp:write",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
    setup();
    expect(
      await screen.findByText(
        /Its mcp:write permissions, listed with its other permissions, also let it connect/,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText("This agent cannot connect to any MCP server."),
    ).toBeNull();
  });
  it("names every server's project, whether or not its grant stores one", async () => {
    // The setup flow stores a server with its project; the permission picker
    // stores the server alone. A stored project wins over the server's own,
    // so the hosted grant stores a different project than its server's.
    mocks.list.mockResolvedValue([
      {
        id: "grant_hosted",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_example",
          projectId: "project_other",
        },
      },
      {
        id: "grant_remote",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server_remote" },
      },
      {
        id: "grant_unknown",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server_gone" },
      },
    ]);
    setup();
    expect(await screen.findByText("Remote server")).toBeTruthy();
    // The hosted row keeps its stored project; the picker row reads its
    // server's project.
    expect(screen.getAllByText("Project: Other project")).toHaveLength(1);
    expect(screen.getAllByText("Project: Example project")).toHaveLength(1);
    // A server the inventory does not know is still listed, with no project
    // and no bare id.
    expect(screen.getByText("server_gone")).toBeTruthy();
    expect(screen.queryByText(/Project: server_gone/)).toBeNull();
  });
  it("shows an org permission as the whole organization unless narrowed to a project", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "grant_sync",
        scope: "org:device_agent_sync",
        selector: { resourceKind: "org", resourceId: "*" },
      },
      {
        id: "grant_hooks",
        scope: "org:hooks_ingest",
        selector: {
          resourceKind: "org",
          resourceId: "*",
          projectId: "project_example",
        },
      },
    ]);
    setup();
    expect(await screen.findByText("org:device_agent_sync")).toBeTruthy();
    expect(screen.getAllByText("Whole organization")).toHaveLength(1);
    expect(screen.getByText("All resources")).toBeTruthy();
    expect(screen.getByText("Project: Example project")).toBeTruthy();
    expect(screen.queryByText(/^on behalf of/)).toBeNull();
  });
  // A wildcard project constrains nothing; an empty one still narrows.
  it.each([
    ["*", "Whole organization", "All resources", 0],
    ["", "All resources", "Whole organization", 1],
  ])(
    "labels an org permission on project %j as %s",
    async (projectId, shown, hidden, projectConstraints) => {
      mocks.list.mockResolvedValue([
        {
          id: "grant_hooks",
          scope: "org:hooks_ingest",
          selector: { resourceKind: "org", resourceId: "*", projectId },
        },
      ]);
      setup();
      expect(await screen.findByText(shown)).toBeTruthy();
      expect(screen.queryByText(hidden)).toBeNull();
      expect(screen.queryAllByText(/^Project:/)).toHaveLength(
        projectConstraints,
      );
    },
  );
  it("shows an org permission stored with the organization's id as the whole organization", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "grant_sync",
        scope: "org:device_agent_sync",
        selector: { resourceKind: "org", resourceId: "org_example" },
      },
    ]);
    setup();
    expect(await screen.findByText("Whole organization")).toBeTruthy();
    expect(screen.queryByText(/org_example/)).toBeNull();
  });
  it("updates after saving permissions in Agent Identity", async () => {
    const client = setup();
    await screen.findByText("No permissions configured for this agent.");
    mocks.list.mockResolvedValue([
      {
        id: "new_grant",
        scope: "project:read",
        selector: { resourceKind: "project", resourceId: "project_example" },
      },
    ]);
    await invalidateAgentPolicy(
      client,
      "org_example",
      "owner_example",
      agent.id,
    );
    expect(await screen.findByText("project:read")).toBeTruthy();
    expect(
      screen.getByText("Example project", { selector: "span" }),
    ).toBeTruthy();
    expect(
      screen.queryByText("No permissions configured for this agent."),
    ).toBeNull();
  });
  it("saves permissions for an agent with a credential restriction", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "project_grant",
        scope: "project:read",
        effect: "allow",
        selector: { resourceKind: "project", resourceId: "*" },
      },
      {
        id: "credential_restriction",
        scope: "mcp:blocked_auth",
        effect: "allow",
        selector: { resourceKind: "mcp_auth_client", resourceId: "client_one" },
      },
    ]);
    setup();
    await screen.findByText("project:read");
    expect(screen.queryByText("mcp:blocked_auth")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Edit permissions" }));
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Edit permissions" }),
      ).toBeNull(),
    );
    expect(
      screen.queryByText(/Permissions changed while you were editing/),
    ).toBeNull();
  });
  it("leaves upstream credential rules to their own panel", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "project_grant",
        scope: "project:read",
        effect: "allow",
        selector: { resourceKind: "project", resourceId: "*" },
      },
      {
        id: "auth_everything",
        scope: "mcp:auth",
        effect: "allow",
        selector: { resourceKind: "mcp_auth_client", resourceId: "*" },
      },
    ]);
    setup();
    await screen.findByText("project:read");
    expect(screen.queryByText("mcp:auth")).toBeNull();
  });
  it("keeps the slugs and says so when the permission catalogue fails to load", async () => {
    mocks.scopesError = true;
    mocks.list.mockResolvedValue([
      {
        id: "inference_grant",
        scope: "inference:execute",
        selector: { resourceKind: "inference", resourceId: "*" },
      },
    ]);
    setup();
    const slug = await screen.findByText("inference:execute");
    expect(slug.parentElement?.textContent).toBe("inference:execute");
    expect(screen.queryByText("Run inference")).toBeNull();
    expect(
      screen.getByText(/Permission names could not be loaded/),
    ).toBeTruthy();
  });
  it("does not claim the agent has no permissions when loading fails", async () => {
    mocks.list.mockRejectedValue(new Error("Unavailable"));
    setup();
    await waitFor(() =>
      expect(screen.getByText(/could not be loaded/i)).toBeTruthy(),
    );
    expect(
      screen.queryByText("No permissions configured for this agent."),
    ).toBeNull();
  });
  it("withholds policy without setup permission", () => {
    setup({ ...agent, permissions: { ...agent.permissions, write: false } });
    expect(
      screen.getByText(/need permission to manage this agent/),
    ).toBeTruthy();
    expect(mocks.list).not.toHaveBeenCalled();
  });
  it("isolates the profile from another agent's cached permissions", async () => {
    const client = new QueryClient();
    client.setQueryData(
      ["agent-policy-grants", "org_example", "other_agent"],
      [
        {
          id: "other_grant",
          scope: "project:write",
          selector: { resourceKind: "project", resourceId: "*" },
        },
      ],
    );
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <AgentIdentityPermissions agent={agent} />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await screen.findByText("No permissions configured for this agent.");
    expect(screen.queryByText("project:write")).toBeNull();
  });
});

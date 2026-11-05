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
vi.mock("@/pages/access/useResourceLinkMaps", () => ({
  useResourceLinkMaps: () => ({
    projectMap: new Map([
      ["project_example", { slug: "example", name: "Example project" }],
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
    mcpServerMap: new Map(),
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
    await screen.findByText("inference:execute");
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
    expect(mocks.list).toHaveBeenCalledWith({ agentId: agent.id }, undefined, {
      signal: expect.any(AbortSignal),
    });
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
  it("says which upstream credentials each mcp:auth grant covers", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "auth_shared",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "client_calendar",
        },
      },
      {
        id: "auth_own",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          clientType: "remote_session",
          connectionOwner: "$represented_user",
        },
      },
      {
        id: "auth_attached",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          connectionOwner: "user_ada",
          upstreamClient: "upstream_notes",
        },
      },
      {
        id: "auth_gone",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          connectionOwner: "d265f01b-0000-4000-8000-000000000000",
          upstreamClient: "01a10332-0000-4000-8000-000000000000",
        },
      },
    ]);
    setup();
    expect(await screen.findByText("mcp:auth")).toBeTruthy();
    expect(
      screen.getByText(
        "Sign in to MCP servers through these upstream credentials.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Calendar server" })
        .getAttribute("href"),
    ).toBe("/org/projects/example/mcp/calendar/settings#authentication");
    expect(
      screen.getByText("Personal connections of the person the agent acts for"),
    ).toBeTruthy();
    expect(
      await screen.findByRole("link", { name: "Ada Lovelace" }),
    ).toBeTruthy();
    expect(mocks.identityLink).toHaveBeenCalledWith({ userId: "user_ada" });
    expect(
      (await screen.findByRole("link", { name: "Meeting notes" })).getAttribute(
        "href",
      ),
    ).toMatch(/\/issuer_notes\/clients\/upstream_notes$/);
    // A person who left is still linked; a removed upstream client is not.
    expect(screen.getByRole("link", { name: "User d265f01b…" })).toBeTruthy();
    expect(mocks.identityLink).toHaveBeenCalledWith({
      userId: "d265f01b-0000-4000-8000-000000000000",
    });
    expect(screen.queryByRole("link", { name: "01a10332…" })).toBeNull();
    // Broadest first, down to one named credential.
    expect(
      screen.getAllByRole("listitem").map((item) => item.textContent),
    ).toEqual([
      "Personal connections of the person the agent acts for",
      "Ada Lovelace's Meeting notes connection · on Notes server",
      "User d265f01b…'s connection through upstream client 01a10332…",
      "Content calendar · Service account · on Calendar server",
    ]);
    expect(screen.queryByText("All resources")).toBeNull();
  });
  it("shows only the grant on any upstream credential, which covers the narrower ones", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "auth_shared",
        scope: "mcp:auth",
        effect: "allow",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "client_calendar",
        },
      },
      {
        id: "auth_everything",
        scope: "mcp:auth",
        effect: "allow",
        selector: { resourceKind: "mcp_auth_client", resourceId: "*" },
      },
      {
        id: "auth_own",
        scope: "mcp:auth",
        effect: "allow",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          connectionOwner: "$represented_user",
        },
      },
      {
        id: "auth_denied",
        scope: "mcp:auth",
        effect: "deny",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "client_calendar",
        },
      },
    ]);
    setup();
    expect(await screen.findByText("mcp:auth")).toBeTruthy();
    // A deny is not covered by the wildcard, so it stays.
    expect(
      screen.getAllByRole("listitem").map((item) => item.textContent),
    ).toEqual([
      "Any upstream credential in the organization",
      "Content calendar · Service account · on Calendar server",
    ]);
  });
  it("falls back to short ids when it may not read the organization", async () => {
    mocks.hasScope.mockImplementation((scope: string) => scope !== "org:read");
    mocks.list.mockResolvedValue([
      {
        id: "auth_shared",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "client_calendar",
        },
      },
      {
        id: "auth_attached",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          connectionOwner: "user_ada",
          upstreamClient: "upstream_notes",
        },
      },
    ]);
    setup();
    expect(await screen.findByText("mcp:auth")).toBeTruthy();
    expect(
      screen.getAllByRole("listitem").map((item) => item.textContent),
    ).toEqual([
      "User user_ada's connection through upstream client upstream…",
      "Upstream credential client_c…",
    ]);
    expect(mocks.allAuthClients).not.toHaveBeenCalledWith({ enabled: true });
    expect(mocks.listMembers).not.toHaveBeenCalled();
    expect(mocks.getUpstream).not.toHaveBeenCalled();
  });
  it("loads no auth clients for grants that name none", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "auth_own",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          clientType: "remote_session",
          connectionOwner: "$represented_user",
        },
      },
      {
        id: "auth_type",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          clientType: "service_account",
        },
      },
    ]);
    setup();
    expect(
      await screen.findByText("Any service account in the organization"),
    ).toBeTruthy();
    expect(mocks.allAuthClients).not.toHaveBeenCalledWith({ enabled: true });
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

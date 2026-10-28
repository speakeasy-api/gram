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

const mocks = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false, error: null }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "owner_example" } }),
  useOrganization: () => ({
    id: "org_example",
    projects: [{ id: "project_example", name: "Example project" }],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { listPolicyGrants: mocks.list } }),
  useSlugs: () => ({ orgSlug: "org" }),
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
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    agents: { href: () => "/org/projects/example/agent-management" },
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
  mocks.list.mockResolvedValue([]);
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

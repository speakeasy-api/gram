import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { AgentPermissionEditor } from "./AgentPermissionEditor";
const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  remove: vi.fn(),
  close: vi.fn<() => void>(),
  allowedProjects: ["project_one", "project_two", "project_three"],
  permissionsLoading: false,
  scopesLoading: false,
  scopesError: false,
  serverGroups: [] as unknown[],
  assistantGroups: [] as unknown[],
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (_scope: string, _resource: string, project: string) =>
      mocks.allowedProjects.includes(project),
    isLoading: mocks.permissionsLoading,
    error: null,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "owner_example" } }),
  useOrganization: () => ({
    id: "org_example",
    projects: [
      { id: "project_one", name: "Project one", slug: "project-one" },
      { id: "project_two", name: "Project two", slug: "project-two" },
      { id: "project_three", name: "Project three", slug: "project-three" },
    ],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    agents: {
      listPolicyGrants: mocks.list,
      createPolicyGrant: mocks.create,
      deletePolicyGrant: mocks.remove,
    },
  }),
}));
const policyScope = (
  slug: string,
  resourceType: string,
  label: string,
  subScopes: string[] = [],
) => ({
  slug,
  label,
  description: `${label}.`,
  resourceType,
  category: resourceType,
  visibility: "user_visible",
  agentEligible: true,
  sensitive: false,
  subScopes,
});
vi.mock("@gram/client/react-query/listAgentPolicyScopes.js", () => ({
  useListAgentPolicyScopes: () => ({
    data:
      mocks.scopesLoading || mocks.scopesError
        ? undefined
        : {
            scopes: [
              policyScope("inference:execute", "inference", "Run inference"),
              policyScope("mcp:connect", "mcp", "Connect to MCP servers"),
              policyScope("mcp:read", "mcp", "View MCP servers", [
                "mcp:connect",
              ]),
              policyScope("mcp:write", "mcp", "Manage MCP servers", [
                "mcp:read",
              ]),
              policyScope("project:read", "project", "View projects"),
              policyScope("skill:read", "skill", "View skills"),
              policyScope("assistant:read", "assistant", "View assistants"),
              policyScope(
                "org:device_agent_sync",
                "org",
                "Sync device agent plugins",
              ),
              policyScope("org:hooks_ingest", "org", "Send hook events"),
            ],
          },
    isLoading: mocks.scopesLoading,
    isError: mocks.scopesError,
  }),
}));
vi.mock("@/pages/access/useOrgMcpServers", () => ({
  useOrgMcpServers: () => ({
    groups: mocks.serverGroups,
    settled: true,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/pages/access/useResourceLinkMaps", () => ({
  useResourceLinkMaps: () => ({
    projectMap: new Map([
      ["project_one", { slug: "project-one", name: "Project one" }],
    ]),
    toolsetMap: new Map([
      [
        "server_docs",
        { slug: "docs", name: "Docs server", projectId: "project_one" },
      ],
    ]),
    mcpServerMap: new Map(),
  }),
}));
vi.mock("@/pages/access/useOrgAssistants", () => ({
  useOrgAssistants: () => ({
    groups: mocks.assistantGroups,
    settled: true,
    isError: false,
    refetch: vi.fn(),
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
const inferenceGrant = (projectId: string): AgentPolicyGrant => ({
  createdAt: new Date(),
  updatedAt: new Date(),
  id: `grant_${projectId}`,
  scope: "inference:execute",
  effect: "allow",
  selector: { resourceKind: "inference", resourceId: "*", projectId },
});
function setup(grants: AgentPolicyGrant[] = [], subject = agent) {
  mocks.list.mockResolvedValue(grants);
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <AgentPermissionEditor
          agent={subject}
          grants={grants}
          onClose={mocks.close}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
function openPlatform() {
  fireEvent.mouseDown(screen.getByRole("tab", { name: /Platform access/ }));
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.allowedProjects = ["project_one", "project_two", "project_three"];
  mocks.permissionsLoading = false;
  mocks.scopesLoading = false;
  mocks.scopesError = false;
  mocks.serverGroups = [];
  mocks.assistantGroups = [];
  mocks.create.mockResolvedValue({});
  mocks.remove.mockResolvedValue({});
});
describe("Agent permission editor", () => {
  it("creates inference grants for multiple projects through the canonical permission picker", async () => {
    setup();
    openPlatform();
    fireEvent.click(screen.getByRole("button", { name: "Add permissions" }));
    fireEvent.click(screen.getByText("inference:execute"));
    fireEvent.click(
      await screen.findByRole("button", { name: /Choose resources/ }),
    );
    expect(screen.queryByText("Specific assistants")).toBeNull();
    expect(screen.queryByText("Specific servers")).toBeNull();
    fireEvent.click(screen.getAllByText("Project one")[0]!);
    fireEvent.click(screen.getByText("Project two"));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).toHaveBeenCalledTimes(2);
    for (const projectId of ["project_one", "project_two"]) {
      expect(mocks.create).toHaveBeenCalledWith({
        createAgentPolicyGrantForm: {
          agentId: agent.id,
          scope: "inference:execute",
          effect: "allow",
          selector: inferenceGrant(projectId).selector,
        },
      });
    }
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it("grants device agent sync to the whole organization", async () => {
    setup();
    openPlatform();
    fireEvent.click(screen.getByRole("button", { name: "Add permissions" }));
    fireEvent.click(screen.getByText("org:device_agent_sync"));
    expect(
      within(
        await screen.findByRole("region", { name: "Changes to save" }),
      ).getByText("Whole organization"),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).toHaveBeenCalledOnce();
    expect(mocks.create).toHaveBeenCalledWith({
      createAgentPolicyGrantForm: {
        agentId: agent.id,
        scope: "org:device_agent_sync",
        effect: "allow",
        selector: { resourceKind: "org", resourceId: "*" },
      },
    });
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it("deletes only the chosen project's grant, preserving the other project, MCP and unsupported grants", async () => {
    const grants: AgentPolicyGrant[] = [
      inferenceGrant("project_one"),
      inferenceGrant("project_two"),
      {
        createdAt: new Date(),
        updatedAt: new Date(),
        id: "mcp_grant",
        scope: "mcp:connect",
        effect: "allow",
        selector: { resourceKind: "mcp", resourceId: "server_example" },
      },
      {
        createdAt: new Date(),
        updatedAt: new Date(),
        id: "unsupported_grant",
        scope: "mcp:read",
        effect: "allow",
        selector: {
          resourceKind: "mcp",
          resourceId: "*",
          serverUrl: "https://example.test/mcp",
        },
      },
    ];
    setup(grants);
    openPlatform();
    fireEvent.click(screen.getByRole("button", { name: /Choose resources/ }));
    fireEvent.click(screen.getAllByText("Project one")[0]!);
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.remove).toHaveBeenCalledExactlyOnceWith({
      deleteAgentPolicyGrantForm: {
        agentId: agent.id,
        grantId: "grant_project_one",
        expectedPolicy: {
          scope: "inference:execute",
          selector: inferenceGrant("project_one").selector,
        },
      },
    });
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("neither lists nor offers mcp:connect, and never writes it", async () => {
    const connect: AgentPolicyGrant = {
      createdAt: new Date(),
      updatedAt: new Date(),
      id: "connect_grant",
      scope: "mcp:connect",
      effect: "allow",
      selector: {
        resourceKind: "mcp",
        resourceId: "server_example",
        projectId: "project_one",
      },
    };
    setup([inferenceGrant("project_one"), connect]);
    expect(screen.queryByText("mcp:connect")).toBeNull();
    expect(screen.queryByRole("tab", { name: /MCP access/ })).toBeNull();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Choose resources for inference:execute",
      }),
    );
    fireEvent.click(screen.getByText("Project two"));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).toHaveBeenCalledExactlyOnceWith({
      createAgentPolicyGrantForm: {
        agentId: agent.id,
        scope: "inference:execute",
        effect: "allow",
        selector: inferenceGrant("project_two").selector,
      },
    });
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it("does not offer mcp:connect among the permissions to add", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Add permissions" }));
    expect(screen.getByText("mcp:read")).toBeTruthy();
    expect(screen.queryByText("mcp:connect")).toBeNull();
  });
  it("rejects a stale base without creating or deleting permissions", async () => {
    setup([inferenceGrant("project_one")]);
    mocks.list.mockResolvedValue([inferenceGrant("project_two")]);
    openPlatform();
    fireEvent.click(screen.getByRole("button", { name: /Choose resources/ }));
    fireEvent.click(screen.getByText("Project three"));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      expect.stringContaining("Permissions changed while you were editing"),
    );
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.close).not.toHaveBeenCalled();
  });
  it("saves against a ceiling holding a credential restriction without showing or removing it", async () => {
    const restriction: AgentPolicyGrant = {
      createdAt: new Date(),
      updatedAt: new Date(),
      id: "credential_restriction",
      scope: "mcp:blocked_auth",
      effect: "allow",
      selector: { resourceKind: "mcp_auth_client", resourceId: "client_one" },
    };
    setup([inferenceGrant("project_one"), restriction]);
    expect(screen.queryByText(/mcp:blocked_auth/)).toBeNull();
    openPlatform();
    fireEvent.click(screen.getByRole("button", { name: /Choose resources/ }));
    fireEvent.click(screen.getByText("Project two"));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).toHaveBeenCalledOnce();
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it("does not normalize redundant grants on a no-op save", async () => {
    setup([
      inferenceGrant("project_one"),
      { ...inferenceGrant("project_one"), id: "duplicate_inference_grant" },
      {
        ...inferenceGrant("project_one"),
        id: "all_inference_grant",
        selector: { resourceKind: "inference", resourceId: "*" },
      },
    ]);
    const invalidate = vi.spyOn(QueryClient.prototype, "invalidateQueries");
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
    // Nothing was written, so nothing is refetched.
    expect(mocks.list).not.toHaveBeenCalled();
    expect(invalidate).not.toHaveBeenCalled();
    invalidate.mockRestore();
  });
  it("disables policy changes without write permission", () => {
    setup([inferenceGrant("project_one")], {
      ...agent,
      permissions: { ...agent.permissions, write: false },
    });
    openPlatform();
    expect(
      (
        screen.getByRole("button", {
          name: "Save permissions",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      (
        screen.getByRole("button", {
          name: "Add permissions",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      (
        screen.getByRole("button", {
          name: "Remove inference:execute",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it("shows the catalogue label beside each permission's slug", () => {
    setup([inferenceGrant("project_one")]);
    openPlatform();
    expect(screen.getByText("Run inference")).toBeTruthy();
    expect(screen.getByText("inference:execute")).toBeTruthy();
    expect(screen.getByText("Run inference.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add permissions" }));
    expect(screen.getByText("View projects")).toBeTruthy();
    expect(screen.getByText("project:read")).toBeTruthy();
    expect(screen.getByText("MCP servers")).toBeTruthy();
  });
  it("withholds the picker and Save while the permission catalogue loads", () => {
    mocks.scopesLoading = true;
    setup([inferenceGrant("project_one")]);
    expect(
      screen.getByText("Loading the permissions an agent can hold…"),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.queryByText("No platform permissions")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Add permissions" }),
    ).toBeNull();
    const save = screen.getByRole("button", {
      name: "Save permissions",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.click(save);
    expect(mocks.list).not.toHaveBeenCalled();
  });
  it("withholds the picker and Save when the permission catalogue fails to load", () => {
    mocks.scopesError = true;
    setup([inferenceGrant("project_one")]);
    expect(screen.getByRole("alert").textContent).toContain(
      "could not be loaded",
    );
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.queryByText("No platform permissions")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Add permissions" }),
    ).toBeNull();
    const save = screen.getByRole("button", {
      name: "Save permissions",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.click(save);
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
  });
});

it("blocks a known unauthorized project before any permission write", async () => {
  mocks.allowedProjects = ["project_one"];
  setup([inferenceGrant("project_one")]);
  openPlatform();
  fireEvent.click(screen.getByRole("button", { name: /Choose resources/ }));
  fireEvent.click(screen.getByText("Project two"));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getByRole("alert").textContent).toContain(
    "You need inference permission in Project two",
  );
  const button = screen.getByRole("button", {
    name: "Save permissions",
  }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  fireEvent.click(button);
  expect(mocks.create).not.toHaveBeenCalled();
  expect(mocks.remove).not.toHaveBeenCalled();
});

it("preserves existing grants after a second addition fails and reloads before retrying", async () => {
  const existing = inferenceGrant("project_one");
  const unrelated: AgentPolicyGrant = {
    ...existing,
    id: "existing_mcp",
    scope: "mcp:connect",
    selector: { resourceKind: "mcp", resourceId: "server_example" },
  };
  setup([existing, unrelated]);
  openPlatform();
  fireEvent.click(screen.getByRole("button", { name: /Choose resources/ }));
  fireEvent.click(screen.getAllByText("Project one")[0]!);
  fireEvent.click(screen.getByText("Project two"));
  fireEvent.click(screen.getByText("Project three"));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  mocks.create
    .mockResolvedValueOnce(inferenceGrant("project_two"))
    .mockRejectedValueOnce(new Error("Forbidden"));
  fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "saved before the error",
  );
  expect(screen.getByRole("alert").textContent).toContain(
    "closing does not undo saved changes",
  );
  expect(mocks.create).toHaveBeenCalledTimes(2);
  expect(mocks.remove).not.toHaveBeenCalled();
  expect(
    (
      screen.getByRole("button", {
        name: "Save permissions",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
  mocks.list.mockResolvedValue([
    existing,
    unrelated,
    inferenceGrant("project_two"),
  ]);
  fireEvent.click(
    screen.getByRole("button", { name: "Reload saved permissions" }),
  );
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  fireEvent.click(screen.getByRole("button", { name: /Choose resources/ }));
  fireEvent.click(screen.getAllByText("Project one")[0]!);
  fireEvent.click(screen.getByText("Project three"));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
  await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
  expect(mocks.create).toHaveBeenCalledTimes(3);
  expect(
    mocks.create.mock.calls[2]?.[0].createAgentPolicyGrantForm.selector
      .projectId,
  ).toBe("project_three");
  expect(mocks.remove).toHaveBeenCalledExactlyOnceWith({
    deleteAgentPolicyGrantForm: {
      agentId: agent.id,
      grantId: existing.id,
      expectedPolicy: { scope: existing.scope, selector: existing.selector },
    },
  });
});

it.each([
  { projectId: undefined, dismiss: "Done" },
  { projectId: "*", dismiss: "Done" },
  { projectId: undefined, dismiss: "Close" },
  { projectId: "*", dismiss: "Close" },
])(
  "preserves existing all-project inference on drawer $dismiss ($projectId)",
  async ({ projectId, dismiss }) => {
    const existing: AgentPolicyGrant = {
      ...inferenceGrant("project_one"),
      id: "all_projects_grant",
      selector: {
        resourceKind: "inference",
        resourceId: "*",
        ...(projectId ? { projectId } : {}),
      },
    };
    setup([existing]);
    openPlatform();
    fireEvent.click(screen.getByRole("button", { name: /Choose resources/ }));
    expect(screen.getByRole("button", { name: /All projects/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: dismiss }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: /Choose resources/ }).textContent,
      ).toContain("All projects"),
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Save permissions",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
  },
);

it("stops deletions on a server 409 and retries only against reloaded policy", async () => {
  const original = inferenceGrant("project_one");
  const second = inferenceGrant("project_two");
  setup([original, second]);
  openPlatform();
  fireEvent.click(
    screen.getByRole("button", { name: "Remove inference:execute" }),
  );
  mocks.remove.mockRejectedValueOnce(
    Object.assign(new Error("Policy grant changed since it was read"), {
      statusCode: 409,
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Policy grant changed since it was read",
  );
  expect(mocks.remove).toHaveBeenCalledExactlyOnceWith({
    deleteAgentPolicyGrantForm: {
      agentId: agent.id,
      grantId: original.id,
      expectedPolicy: { scope: original.scope, selector: original.selector },
    },
  });
  expect(mocks.create).not.toHaveBeenCalled();
  expect(
    (
      screen.getByRole("button", {
        name: "Save permissions",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(mocks.close).not.toHaveBeenCalled();

  const updated = {
    ...original,
    selector: inferenceGrant("project_three").selector,
  };
  mocks.list.mockResolvedValue([updated, second]);
  fireEvent.click(
    screen.getByRole("button", { name: "Reload saved permissions" }),
  );
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  expect(mocks.remove).toHaveBeenCalledTimes(1);
  fireEvent.click(
    screen.getByRole("button", { name: "Remove inference:execute" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
  await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
  expect(mocks.remove).toHaveBeenCalledTimes(3);
  expect(mocks.remove.mock.calls[1]?.[0]).toEqual({
    deleteAgentPolicyGrantForm: {
      agentId: agent.id,
      grantId: updated.id,
      expectedPolicy: { scope: updated.scope, selector: updated.selector },
    },
  });
  expect(mocks.remove.mock.calls[2]?.[0]).toEqual({
    deleteAgentPolicyGrantForm: {
      agentId: agent.id,
      grantId: second.id,
      expectedPolicy: { scope: second.scope, selector: second.selector },
    },
  });
  expect(mocks.create).not.toHaveBeenCalled();
});

describe("loading an agent's grants exactly as stored", () => {
  const stored = (
    id: string,
    scope: string,
    selector: AgentPolicyGrant["selector"],
  ): AgentPolicyGrant => ({
    createdAt: new Date(),
    updatedAt: new Date(),
    id,
    scope,
    effect: "allow",
    selector,
  });
  // The shape the agent setup flow writes: each server grant carries the
  // server's project beside its id.
  const agentGrants: AgentPolicyGrant[] = [
    stored("connect_docs", "mcp:connect", {
      resourceKind: "mcp",
      resourceId: "server_docs",
      projectId: "project_one",
    }),
    stored("connect_ops", "mcp:connect", {
      resourceKind: "mcp",
      resourceId: "server_ops",
      projectId: "project_one",
    }),
    stored("auth_any", "mcp:auth", {
      resourceKind: "mcp_auth_client",
      resourceId: "*",
    }),
    stored("read_docs", "mcp:read", {
      resourceKind: "mcp",
      resourceId: "server_docs",
      projectId: "project_one",
    }),
    stored("project_read", "project:read", {
      resourceKind: "project",
      resourceId: "project_one",
    }),
    stored("skill_read", "skill:read", {
      resourceKind: "skill",
      resourceId: "project_two",
    }),
    stored("inference", "inference:execute", {
      resourceKind: "inference",
      resourceId: "*",
      projectId: "project_one",
    }),
    stored("assistant_read", "assistant:read", {
      resourceKind: "assistant",
      resourceId: "assistant_one",
      projectId: "project_one",
    }),
    stored("device_sync", "org:device_agent_sync", {
      resourceKind: "org",
      resourceId: "*",
    }),
    stored("hooks", "org:hooks_ingest", {
      resourceKind: "org",
      resourceId: "org_example",
      projectId: "project_one",
    }),
    stored("environment_read", "environment:read", {
      resourceKind: "environment",
      resourceId: "*",
      projectId: "project_one",
    }),
  ];
  const server = (id: string, name: string) => ({
    id,
    name,
    slug: id,
    tools: [],
    dynamicTools: false,
    remoteBacked: false,
  });
  beforeEach(() => {
    mocks.serverGroups = [
      {
        projectId: "project_one",
        projectName: "Project one",
        servers: [
          server("server_docs", "Docs server"),
          server("server_ops", "Ops server"),
        ],
      },
    ];
    mocks.assistantGroups = [
      {
        projectId: "project_one",
        projectName: "Project one",
        assistants: [{ id: "assistant_one", name: "Helper assistant" }],
      },
    ];
  });
  const rule = (scope: string) =>
    screen.getByRole("button", { name: `Choose resources for ${scope}` });
  const closePicker = () => {
    const closes = screen.getAllByRole("button", { name: "Close" });
    fireEvent.click(closes[closes.length - 1]!);
  };

  it("shows each grant's resources as stored and keeps the rest", () => {
    setup(agentGrants);
    expect(rule("mcp:read").textContent).toBe("1 server");
    expect(rule("project:read").textContent).toBe("Project: Project one");
    expect(rule("skill:read").textContent).toBe("Project: Project two");
    expect(rule("inference:execute").textContent).toBe("Project: Project one");
    expect(rule("assistant:read").textContent).toBe("1 assistant");
    expect(screen.queryByText("mcp:connect")).toBeNull();
    expect(screen.getByText(/Kept as they are/)).toBeTruthy();
    for (const slug of ["mcp:auth", "org:hooks_ingest", "environment:read"]) {
      expect(
        screen.getByRole("button", { name: `Remove ${slug}` }),
      ).toBeTruthy();
    }
  });

  it("lists a kept scope's removal before saving, and removes all its grants", async () => {
    setup(agentGrants);
    fireEvent.click(
      screen.getByRole("button", { name: "Remove environment:read" }),
    );
    const changes = screen.getByRole("region", { name: "Changes to save" });
    expect(changes.textContent).toContain("Removes");
    expect(changes.textContent).toContain("environment:read");
    mocks.list.mockResolvedValue(agentGrants);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.remove).toHaveBeenCalledExactlyOnceWith({
      deleteAgentPolicyGrantForm: {
        agentId: agent.id,
        grantId: "environment_read",
        expectedPolicy: {
          scope: "environment:read",
          selector: agentGrants[10]!.selector,
        },
      },
    });
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("keeps a kept scope again when its removal is undone", () => {
    setup(agentGrants);
    fireEvent.click(screen.getByRole("button", { name: "Remove mcp:auth" }));
    fireEvent.click(screen.getByRole("button", { name: "Keep mcp:auth" }));
    expect(
      screen.queryByRole("region", { name: "Changes to save" }),
    ).toBeNull();
  });

  it("names an org grant stored with the organization's id as the whole organization", () => {
    setup([
      stored("device_sync", "org:device_agent_sync", {
        resourceKind: "org",
        resourceId: "org_example",
      }),
    ]);
    openPlatform();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove org:device_agent_sync" }),
    );
    const changes = screen.getByRole("region", { name: "Changes to save" });
    expect(changes.textContent).toContain("Whole organization");
    expect(changes.textContent).not.toContain("org_example");
  });

  it("opens a named server's picker on that server, ticked", () => {
    setup(agentGrants);
    fireEvent.click(rule("mcp:read"));
    for (const box of screen.getAllByRole("checkbox", {
      name: /Docs server/,
    })) {
      expect(box.getAttribute("aria-checked")).toBe("true");
    }
    expect(
      screen
        .getByRole("checkbox", { name: /Ops server/ })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(screen.queryByRole("checkbox", { name: "Project one" })).toBeNull();
  });

  it("keeps a grant when its picker closes without Done, and saves nothing", async () => {
    setup(agentGrants);
    fireEvent.click(rule("mcp:read"));
    fireEvent.click(screen.getByRole("button", { name: /Specific projects/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Project one" }));
    closePicker();
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Choose resources" }),
      ).toBeNull(),
    );
    expect(rule("mcp:read").textContent).toBe("1 server");
    expect(
      screen.queryByRole("region", { name: "Changes to save" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
  });

  it("writes nothing after every picker is opened and confirmed unchanged", async () => {
    setup(agentGrants);
    const scopes = screen
      .getAllByRole("button", { name: /^Choose resources for / })
      .map((button) =>
        button.getAttribute("aria-label")!.replace("Choose resources for ", ""),
      );
    expect(scopes).toEqual(
      expect.arrayContaining([
        "mcp:read",
        "project:read",
        "skill:read",
        "inference:execute",
        "assistant:read",
      ]),
    );
    for (const scope of scopes) {
      fireEvent.click(rule(scope));
      fireEvent.click(screen.getByRole("button", { name: "Done" }));
      await waitFor(() =>
        expect(
          screen.queryByRole("dialog", { name: "Choose resources" }),
        ).toBeNull(),
      );
    }
    expect(
      screen.queryByRole("region", { name: "Changes to save" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
  });

  it("lists a removal before saving it, and removes only that grant", async () => {
    setup(agentGrants);
    fireEvent.click(screen.getByRole("button", { name: "Remove mcp:read" }));
    const changes = screen.getByRole("region", { name: "Changes to save" });
    expect(changes.textContent).toContain("Removes");
    expect(changes.textContent).toContain("mcp:read");
    expect(changes.textContent).toContain("Docs server in Project one");
    mocks.list.mockResolvedValue(agentGrants);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.remove).toHaveBeenCalledExactlyOnceWith({
      deleteAgentPolicyGrantForm: {
        agentId: agent.id,
        grantId: "read_docs",
        expectedPolicy: {
          scope: "mcp:read",
          selector: agentGrants[3]!.selector,
        },
      },
    });
    expect(mocks.create).not.toHaveBeenCalled();
  });
});

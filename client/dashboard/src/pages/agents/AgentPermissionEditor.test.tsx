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
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { AgentPermissionEditor } from "./AgentPermissionEditor";
const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  remove: vi.fn(),
  close: vi.fn<() => void>(),
  allowedProjects: ["project_one", "project_two", "project_three"],
  permissionsLoading: false,
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
vi.mock("@/pages/access/useOrgMcpServers", () => ({
  useOrgMcpServers: () => ({
    groups: [],
    settled: true,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/pages/access/useOrgAssistants", () => ({
  useOrgAssistants: () => ({
    groups: [],
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
    expect(await screen.findByText("Whole organization")).toBeTruthy();
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
  it("rejects a stale base without creating or deleting permissions", async () => {
    setup([inferenceGrant("project_one")]);
    mocks.list.mockResolvedValue([inferenceGrant("project_two")]);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      expect.stringContaining("Permissions changed while you were editing"),
    );
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.close).not.toHaveBeenCalled();
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
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.close).toHaveBeenCalledOnce());
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
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

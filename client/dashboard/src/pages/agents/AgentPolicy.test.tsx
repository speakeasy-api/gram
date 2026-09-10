import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
} from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import type { ScopeRule } from "@/pages/access/types";
import AgentsPage from "./Agents";

const mocks = vi.hoisted(() => ({
  params: new URLSearchParams(),
  navigate: vi.fn(),
  organizationId: "org_example",
  userId: "user_owner",
  list: vi.fn(),
  detail: vi.fn(),
  listPolicyGrants: vi.fn(),
  createPolicyGrant: vi.fn(),
  deletePolicyGrant: vi.fn(),
  createAgent: vi.fn(),
  agent: {
    id: "agent_example",
    name: "Example agent",
    ownerUserId: "user_owner",
    lifecycle: "active",
    permissions: { read: true, write: true, authorize: true, transfer: true },
  },
}));

// The reused access controls carry their own tests. Here they stand in as the
// smallest surface that still exercises every callback this page wires up, so
// the assertions are about the request the page builds rather than about a
// dropdown opening.
vi.mock("@/pages/access/RolePermissionsSection", () => ({
  RolePermissionsSection: ({
    groups,
    selectedScopes,
    disabled,
    onToggleScope,
    renderScopeRule,
    subjectLabel,
  }: {
    groups: { scopes: ScopeDefinition[] }[];
    selectedScopes: Set<string>;
    disabled?: boolean;
    onToggleScope: (scope: string) => void;
    renderScopeRule: (scope: ScopeDefinition) => ReactNode;
    subjectLabel?: string;
  }) => (
    <div>
      <span>subject:{subjectLabel}</span>
      {groups
        .flatMap((group) => group.scopes)
        .map((scope) => (
          <div key={scope.slug}>
            <button
              type="button"
              disabled={disabled}
              onClick={() => onToggleScope(scope.slug)}
            >
              {selectedScopes.has(scope.slug)
                ? `Remove ${scope.slug}`
                : `Add ${scope.slug}`}
            </button>
            {selectedScopes.has(scope.slug) && renderScopeRule(scope)}
          </div>
        ))}
    </div>
  ),
}));
vi.mock("@/pages/access/PermissionScopeControl", () => ({
  PermissionScopeControl: ({
    allowRule,
    allowLabel,
    disabled,
    canAddException,
    denyRules,
    onChooseSpecific,
    onResetToAll,
  }: {
    allowRule: ScopeRule;
    allowLabel: string;
    disabled?: boolean;
    canAddException: boolean;
    denyRules: unknown[];
    onChooseSpecific: () => void;
    onResetToAll: () => void;
  }) => (
    <span>
      <span>{`applies ${allowRule.id}: ${allowLabel}`}</span>
      <span>{`exceptions ${allowRule.id}: ${canAddException ? "on" : "off"}/${denyRules.length}`}</span>
      <button type="button" disabled={disabled} onClick={onChooseSpecific}>
        {`Narrow ${allowRule.id}`}
      </button>
      <button type="button" disabled={disabled} onClick={onResetToAll}>
        {`Widen ${allowRule.id}`}
      </button>
    </span>
  ),
}));
vi.mock("@/pages/access/GrantRuleDrawerContent", () => ({
  GrantRuleDrawerContent: ({
    scope,
    resourceType,
    onChangeSelectors,
  }: {
    scope?: string;
    resourceType: string;
    onChangeSelectors: (selectors: Selector[] | null) => void;
  }) => (
    <div>
      <span>{`picker ${scope} (${resourceType})`}</span>
      <button
        type="button"
        onClick={() =>
          onChangeSelectors([
            { resourceKind: "mcp", resourceId: "server_one", tool: "search" },
          ])
        }
      >
        Pick server one
      </button>
      <button type="button" onClick={() => onChangeSelectors(null)}>
        Pick all servers
      </button>
    </div>
  ),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: mocks.organizationId,
    slug: "example",
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
  useSession: () => ({
    user: {
      id: mocks.userId,
      displayName: "Owner",
      email: "owner@example.test",
    },
    organizationOverride: false,
    impersonatorEmail: undefined,
  }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    agents: {
      list: mocks.list,
      get: mocks.detail,
      listPolicyGrants: mocks.listPolicyGrants,
      createPolicyGrant: mocks.createPolicyGrant,
      deletePolicyGrant: mocks.deletePolicyGrant,
    },
  }),
}));
vi.mock("@gram/client/react-query/createAgent.js", () => ({
  useCreateAgentMutation: (options: object) =>
    useMutation({ mutationFn: mocks.createAgent, ...options }),
}));
vi.mock("@gram/client/react-query/renameAgent.js", () => ({
  useRenameAgentMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsDelete.js", () => ({
  useAgentsDeleteMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsResume.js", () => ({
  useAgentsResumeMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsRevoke.js", () => ({
  useAgentsRevokeMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsSuspend.js", () => ({
  useAgentsSuspendMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("./AgentAPIKeys", () => ({ AgentAPIKeys: () => <div>API keys</div> }));
vi.mock("./ManagedAgentSessions", () => ({
  ManagedAgentSessions: () => <div>Sessions</div>,
}));
vi.mock("@/components/ui/Avatar", () => ({
  Avatar: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  AvatarImage: ({ src, alt }: { src?: string; alt: string }) => (
    <img src={src} alt={alt} />
  ),
  AvatarFallback: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => null,
}));
vi.mock("react-router", () => ({
  useSearchParams: () => [mocks.params, mocks.navigate],
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/components/page-templates", () => {
  const Part = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  const Section = Object.assign(Part, {
    Header: Part,
    Title: Part,
    Description: Part,
    Panel: Part,
    Body: Part,
    Footer: Part,
  });
  const Page = ({
    children,
    title,
    description,
  }: {
    children: ReactNode;
    title: string;
    description: string;
  }) => (
    <div>
      <h1>{title}</h1>
      <p>{description}</p>
      {children}
    </div>
  );
  return {
    ResourceListPage: Page,
    SettingsPage: Page,
    FormPage: Page,
    SettingsSection: Section,
    DangerSettingsSection: Section,
  };
});

function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <AgentsPage />
    </QueryClientProvider>,
  );
  return {
    ...view,
    client,
    rerenderPage: () =>
      view.rerender(
        <QueryClientProvider client={client}>
          <AgentsPage />
        </QueryClientProvider>,
      ),
  };
}

function createdAgentForm() {
  return mocks.createAgent.mock.calls[0]?.[0]?.request?.createAgentForm;
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.params = new URLSearchParams({ create: "true" });
  mocks.organizationId = "org_example";
  mocks.userId = "user_owner";
  mocks.agent.ownerUserId = "user_owner";
  mocks.agent.permissions = {
    read: true,
    write: true,
    authorize: true,
    transfer: true,
  };
  mocks.list.mockResolvedValue([mocks.agent]);
  mocks.detail.mockResolvedValue(mocks.agent);
  mocks.listPolicyGrants.mockResolvedValue([]);
  mocks.createPolicyGrant.mockResolvedValue({});
  mocks.deletePolicyGrant.mockResolvedValue(undefined);
  mocks.createAgent.mockResolvedValue({ ...mocks.agent, id: "agent_new" });
});

describe("Creating an agent with permissions", () => {
  it("offers only agent-runtime-safe permissions, named for the agent", () => {
    setup();
    expect(screen.getByText("subject:this agent")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Add mcp:connect" }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add org:admin" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Add risk_policy:bypass" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Add mcp:blocked_connect" }),
    ).toBeNull();
  });

  it("creates the agent and its MCP permission in one request", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "  Release assistant  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    expect(createdAgentForm()).toEqual({
      name: "Release assistant",
      policyGrants: [
        {
          effect: "allow",
          scope: "mcp:connect",
          selector: { resourceKind: "mcp", resourceId: "*" },
        },
      ],
    });
    // Nothing is created before the single atomic request, so no separate
    // grant call may exist.
    expect(mocks.createPolicyGrant).not.toHaveBeenCalled();
  });

  it("sends the narrowed selector the picker produced", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Narrowed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    expect(screen.getByText("applies mcp:connect: All servers")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Narrow mcp:connect" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Pick server one" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    expect(createdAgentForm().policyGrants).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_one",
          tool: "search",
        },
      },
    ]);
  });

  it("never offers an exception, because agent policy is allow-only", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    expect(screen.getByText("exceptions mcp:connect: off/0")).toBeTruthy();
  });

  it("gives environment and risk policy permissions no resource choice", () => {
    setup();
    fireEvent.click(
      screen.getByRole("button", { name: "Add environment:read" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Add risk_policy:evaluate" }),
    );
    expect(
      screen.queryByRole("button", { name: "Narrow environment:read" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Narrow risk_policy:evaluate" }),
    ).toBeNull();
  });

  it("refuses a permissionless agent until it is confirmed, then sends no grants", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Bare" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    expect(mocks.createAgent).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(
      /Add permissions or explicitly confirm/,
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Create without permissions/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    expect(createdAgentForm()).toEqual({ name: "Bare" });
  });

  it("keeps the whole draft when the atomic create is rejected", async () => {
    mocks.createAgent.mockRejectedValue(
      new Error("scope is not allowed for agent policy"),
    );
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Retryable" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toMatch(
        /scope is not allowed/,
      ),
    );
    expect(mocks.navigate).not.toHaveBeenCalled();
    expect(
      (screen.getByLabelText("Agent name") as HTMLInputElement).value,
    ).toBe("Retryable");
    expect(
      screen.getByRole("button", { name: "Remove mcp:connect" }),
    ).toBeTruthy();

    mocks.createAgent.mockResolvedValue({ ...mocks.agent, id: "agent_new" });
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(2));
    expect(
      mocks.createAgent.mock.calls[1]?.[0].request.createAgentForm,
    ).toEqual({
      name: "Retryable",
      policyGrants: [
        {
          effect: "allow",
          scope: "mcp:connect",
          selector: { resourceKind: "mcp", resourceId: "*" },
        },
      ],
    });
  });

  it("drops the stale delegable candidate set for the new agent", async () => {
    const { client } = setup();
    client.setQueryData(
      ["agent-delegable-grants", "org_example", "user_owner", "agent_new"],
      [],
    );
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Fresh" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith({ id: "agent_new" }),
    );
    await waitFor(() =>
      expect(
        client.getQueryState([
          "agent-delegable-grants",
          "org_example",
          "user_owner",
          "agent_new",
        ])?.isInvalidated,
      ).toBe(true),
    );
  });
});

describe("Editing an existing agent's permissions", () => {
  beforeEach(() => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
  });

  it("says a permissionless agent cannot authorize anything", async () => {
    setup();
    expect(
      await screen.findByText(/This agent has no permissions/),
    ).toBeTruthy();
  });

  it("applies an added permission and refreshes key discovery", async () => {
    const { client } = setup();
    client.setQueryData(
      ["agent-delegable-grants", "org_example", "user_owner", "agent_example"],
      [],
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(mocks.createPolicyGrant).toHaveBeenCalledTimes(1),
    );
    expect(mocks.createPolicyGrant.mock.calls[0]?.[0]).toEqual({
      createAgentPolicyGrantForm: {
        agentId: "agent_example",
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    });
    expect(mocks.deletePolicyGrant).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(
        client.getQueryState([
          "agent-delegable-grants",
          "org_example",
          "user_owner",
          "agent_example",
        ])?.isInvalidated,
      ).toBe(true),
    );
  });

  it("replaces a widened permission by removing the narrower grant first", async () => {
    mocks.listPolicyGrants.mockResolvedValue([
      {
        id: "grant_one",
        scope: "mcp:connect",
        effect: "allow",
        selector: { resourceKind: "mcp", resourceId: "server_two" },
        createdAt: new Date(0),
        updatedAt: new Date(0),
      },
    ]);
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Widen mcp:connect" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(mocks.createPolicyGrant).toHaveBeenCalledTimes(1),
    );
    expect(mocks.deletePolicyGrant.mock.calls[0]?.[0]).toEqual({
      agentPolicyGrantIDForm: {
        agentId: "agent_example",
        grantId: "grant_one",
      },
    });
    expect(mocks.deletePolicyGrant.mock.invocationCallOrder[0]!).toBeLessThan(
      mocks.createPolicyGrant.mock.invocationCallOrder[0]!,
    );
  });

  it("shows the stored ceiling, not the draft, when a change part-way fails", async () => {
    mocks.createPolicyGrant.mockRejectedValue(new Error("Rejected."));
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toMatch(
        /Some changes may not have been applied/,
      ),
    );
    expect(
      screen.getByRole("button", { name: "Add mcp:connect" }),
    ).toBeTruthy();
  });

  it("lets the owner configure policy without any RBAC grant", async () => {
    setup();
    const add = await screen.findByRole("button", { name: "Add mcp:connect" });
    expect((add as HTMLButtonElement).disabled).toBe(false);
    expect(
      screen.getByText(/Permission changes are recorded in the organization/),
    ).toBeTruthy();
  });

  it("locks the editor for a viewer who lost write permission", async () => {
    mocks.agent.ownerUserId = "user_another";
    mocks.agent.permissions = {
      read: true,
      write: false,
      authorize: false,
      transfer: false,
    };
    setup();
    const add = await screen.findByRole("button", { name: "Add mcp:connect" });
    expect((add as HTMLButtonElement).disabled).toBe(true);
    expect(
      screen.getByText(
        /do not have permission to change this agent's permissions/,
      ),
    ).toBeTruthy();
    expect(
      (
        screen.getByRole("button", {
          name: "Save permissions",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("keeps one organization's ceiling out of another's cache", async () => {
    const view = setup();
    await screen.findByRole("button", { name: "Add mcp:connect" });
    expect(
      view.client.getQueryData([
        "agent-policy-grants",
        "org_example",
        "agent_example",
      ]),
    ).toBeTruthy();

    mocks.organizationId = "org_other";
    mocks.listPolicyGrants.mockReturnValue(new Promise(() => {}));
    view.rerenderPage();
    await screen.findByText("Loading permissions…");
    const query = view.client.getQueryCache().find({
      queryKey: ["agent-policy-grants", "org_other", "agent_example"],
    });
    expect(query?.state.data).toBeUndefined();
    expect(query?.queryHash).toContain("org_other");
  });
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("Draft ownership across context changes", () => {
  it("drops an unfinished create draft and its open picker when the organization changes", async () => {
    const view = setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Half-written" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    fireEvent.click(screen.getByRole("button", { name: "Narrow mcp:connect" }));
    expect(await screen.findByText("picker mcp:connect (mcp)")).toBeTruthy();

    mocks.organizationId = "org_other";
    view.rerenderPage();

    expect(screen.queryByText("picker mcp:connect (mcp)")).toBeNull();
    expect(
      (screen.getByLabelText("Agent name") as HTMLInputElement).value,
    ).toBe("");
    expect(
      screen.getByRole("button", { name: "Add mcp:connect" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it("drops an unfinished create draft when the signed-in user changes", () => {
    const view = setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Someone else's draft" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));

    mocks.userId = "user_other";
    view.rerenderPage();

    expect(
      (screen.getByLabelText("Agent name") as HTMLInputElement).value,
    ).toBe("");
    expect(
      screen.getByRole("button", { name: "Add mcp:connect" }),
    ).toBeTruthy();
  });

  it("drops a dirty policy draft and closes the picker when write access is lost", async () => {
    const view = setup();
    mocks.params = new URLSearchParams({ id: "agent_example" });
    view.rerenderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Narrow mcp:connect" }));
    expect(await screen.findByText("picker mcp:connect (mcp)")).toBeTruthy();

    mocks.agent.permissions = {
      read: true,
      write: false,
      authorize: false,
      transfer: false,
    };
    view.rerenderPage();

    expect(screen.queryByText("picker mcp:connect (mcp)")).toBeNull();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add mcp:connect" }),
      ).toBeTruthy(),
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Save permissions",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);

    // Regaining the permission must not resurrect the abandoned edit.
    mocks.agent.permissions = {
      read: true,
      write: true,
      authorize: true,
      transfer: true,
    };
    view.rerenderPage();
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Add mcp:connect",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    expect(mocks.createPolicyGrant).not.toHaveBeenCalled();
  });

  it("refuses to open the picker for a viewer who cannot write", async () => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
    mocks.agent.permissions = {
      read: true,
      write: false,
      authorize: false,
      transfer: false,
    };
    mocks.listPolicyGrants.mockResolvedValue([
      {
        id: "grant_one",
        scope: "mcp:connect",
        effect: "allow",
        selector: { resourceKind: "mcp", resourceId: "server_two" },
        createdAt: new Date(0),
        updatedAt: new Date(0),
      },
    ]);
    setup();
    const narrow = await screen.findByRole("button", {
      name: "Narrow mcp:connect",
    });
    expect((narrow as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(narrow);
    expect(screen.queryByText("picker mcp:connect (mcp)")).toBeNull();
  });
});

describe("Confirming the stored ceiling after a save", () => {
  beforeEach(() => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
  });

  it("keeps the editor locked until the confirming read resolves", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    const pending = deferred<unknown[]>();
    // The pre-save read agrees with the pinned base; the confirming read after
    // the writes is the one left hanging.
    mocks.listPolicyGrants
      .mockResolvedValueOnce([])
      .mockReturnValueOnce(pending.promise);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() => expect(mocks.createPolicyGrant).toHaveBeenCalled());

    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Remove mcp:connect",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(true),
    );
    expect(screen.getByRole("button", { name: "Saving…" })).toBeTruthy();

    pending.resolve([
      {
        id: "grant_new",
        scope: "mcp:connect",
        effect: "allow",
        selector: { resourceKind: "mcp", resourceId: "*" },
        createdAt: new Date(0),
        updatedAt: new Date(0),
      },
    ]);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Remove mcp:connect" }),
      ).toBeTruthy(),
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Save permissions",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("fails closed with no editable base when the confirming read fails after a partial write", async () => {
    mocks.listPolicyGrants.mockResolvedValueOnce([]);
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:read" }));
    mocks.createPolicyGrant
      .mockResolvedValueOnce({})
      .mockRejectedValueOnce(new Error("Rejected."));
    mocks.listPolicyGrants
      .mockResolvedValueOnce([])
      .mockRejectedValue(new Error("read failed"));
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));

    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toMatch(
        /Could not confirm this agent's stored permissions/,
      ),
    );
    // No editor at all: a base built from the pre-save cache would be stale.
    expect(
      screen.queryByRole("button", { name: "Add mcp:connect" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Save permissions" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "Reload permissions" }),
    ).toBeTruthy();

    mocks.listPolicyGrants.mockResolvedValue([
      {
        id: "grant_new",
        scope: "mcp:connect",
        effect: "allow",
        selector: { resourceKind: "mcp", resourceId: "*" },
        createdAt: new Date(0),
        updatedAt: new Date(0),
      },
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Reload permissions" }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Remove mcp:connect" }),
      ).toBeTruthy(),
    );
    // The abandoned half of the edit is gone, not carried over.
    expect(screen.getByRole("button", { name: "Add mcp:read" })).toBeTruthy();
  });
});

describe("Stored constraints the editor cannot show", () => {
  beforeEach(() => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
    mocks.listPolicyGrants.mockResolvedValue([
      {
        id: "grant_risk",
        scope: "risk_policy:evaluate",
        effect: "allow",
        selector: {
          resourceKind: "risk_policy",
          resourceId: "*",
          serverUrl: "https://mcp.example.test",
          serverIdentity: "identity_one",
        },
        createdAt: new Date(0),
        updatedAt: new Date(0),
      },
    ]);
  });

  it("says the grant is left as stored and does not offer its scope", async () => {
    setup();
    expect(
      await screen.findByText(/use constraints this editor cannot show/),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Add risk_policy:evaluate" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Remove risk_policy:evaluate" }),
    ).toBeNull();
  });

  it("does not touch it when an unrelated permission is saved", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:read" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(mocks.createPolicyGrant).toHaveBeenCalledTimes(1),
    );
    expect(mocks.deletePolicyGrant).not.toHaveBeenCalled();
    expect(mocks.createPolicyGrant.mock.calls[0]?.[0]).toEqual({
      createAgentPolicyGrantForm: {
        agentId: "agent_example",
        effect: "allow",
        scope: "mcp:read",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    });
  });
});

describe("Choosing the unrestricted option inside the picker", () => {
  it("keeps Done available and saves an unrestricted grant", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Unrestricted" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    fireEvent.click(screen.getByRole("button", { name: "Narrow mcp:connect" }));
    await screen.findByText("picker mcp:connect (mcp)");

    // An unfinished narrowing blocks Done; the unrestricted choice must not.
    expect(
      (screen.getByRole("button", { name: "Done" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Pick all servers" }));
    const done = screen.getByRole("button", { name: "Done" });
    expect((done as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(done);

    expect(screen.getByText("applies mcp:connect: All servers")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    expect(createdAgentForm().policyGrants).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
  });

  it("widens a narrowed permission back to unrestricted", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Rewidened" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add mcp:connect" }));
    fireEvent.click(screen.getByRole("button", { name: "Narrow mcp:connect" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Pick server one" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.getByText("applies mcp:connect: search")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Narrow mcp:connect" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Pick all servers" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.getByText("applies mcp:connect: All servers")).toBeTruthy();
  });
});

describe("A concurrent change by another administrator", () => {
  const otherAdminGrant = {
    id: "grant_other",
    scope: "skill:read",
    effect: "allow",
    selector: { resourceKind: "skill", resourceId: "*" },
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };

  beforeEach(() => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
  });

  it("blocks and writes nothing when the server has a grant the cache never saw", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    // Their grant lands on the server. Nothing refetched it, so the cache still
    // matches the pinned base — only a fresh read can catch this.
    mocks.listPolicyGrants.mockResolvedValue([otherAdminGrant]);
    expect(
      screen.queryByRole("button", { name: "Remove skill:read" }),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toMatch(
        /Someone else changed this agent's permissions/,
      ),
    );
    // Nothing at all was sent: no delete could reach their grant.
    expect(mocks.deletePolicyGrant).not.toHaveBeenCalled();
    expect(mocks.createPolicyGrant).not.toHaveBeenCalled();
    // Fail closed — no editable base until the user reloads.
    expect(
      screen.queryByRole("button", { name: "Save permissions" }),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Reload permissions" }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Remove skill:read" }),
      ).toBeTruthy(),
    );
    expect(
      screen.getByRole("button", { name: "Add mcp:connect" }),
    ).toBeTruthy();
  });

  it("blocks and writes nothing when the pre-save read fails", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    mocks.listPolicyGrants.mockRejectedValue(new Error("read failed"));

    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toMatch(
        /Could not read this agent's current permissions, so nothing was saved/,
      ),
    );
    expect(mocks.deletePolicyGrant).not.toHaveBeenCalled();
    expect(mocks.createPolicyGrant).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: "Save permissions" }),
    ).toBeNull();
  });

  it("still applies a deliberate removal when nothing else changed", async () => {
    // The save diffs against the base pinned when the draft opened, so an
    // unchanged background refetch neither blocks it nor alters what it sends.
    mocks.listPolicyGrants.mockResolvedValue([otherAdminGrant]);
    const { client } = setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Remove skill:read" }),
    );
    client.setQueryData(
      ["agent-policy-grants", "org_example", "agent_example"],
      [{ ...otherAdminGrant }],
    );

    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(mocks.deletePolicyGrant).toHaveBeenCalledTimes(1),
    );
    expect(mocks.deletePolicyGrant.mock.calls[0]?.[0]).toEqual({
      agentPolicyGrantIDForm: {
        agentId: "agent_example",
        grantId: "grant_other",
      },
    });
    expect(mocks.createPolicyGrant).not.toHaveBeenCalled();
  });

  it("still saves when the refetch returns an identical ceiling", async () => {
    mocks.listPolicyGrants.mockResolvedValue([otherAdminGrant]);
    const { client } = setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Add mcp:connect" }),
    );
    // Same grants, new array identity — a plain background refetch.
    client.setQueryData(
      ["agent-policy-grants", "org_example", "agent_example"],
      [{ ...otherAdminGrant }],
    );

    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(mocks.createPolicyGrant).toHaveBeenCalledTimes(1),
    );
    expect(mocks.deletePolicyGrant).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("Losing the editor part-way through a multi-grant save", () => {
  const storedGrants = [
    {
      id: "grant_one",
      scope: "mcp:connect",
      effect: "allow",
      selector: { resourceKind: "mcp", resourceId: "server_one" },
      createdAt: new Date(0),
      updatedAt: new Date(0),
    },
    {
      id: "grant_two",
      scope: "skill:read",
      effect: "allow",
      selector: { resourceKind: "skill", resourceId: "*" },
      createdAt: new Date(0),
      updatedAt: new Date(0),
    },
  ];
  const policyKey = ["agent-policy-grants", "org_example", "agent_example"];
  const delegableKey = [
    "agent-delegable-grants",
    "org_example",
    "user_owner",
    "agent_example",
  ];

  beforeEach(() => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
    mocks.listPolicyGrants.mockResolvedValue(storedGrants);
  });

  it("finishes only the request already in flight, then clears the caches it made untrustworthy", async () => {
    const { client, rerenderPage } = setup();
    // Two removals, so there is a second request to prove never happens.
    fireEvent.click(
      await screen.findByRole("button", { name: "Remove mcp:connect" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove skill:read" }));

    // The API key dialog's candidate set, primed before the save.
    client.setQueryData(delegableKey, []);

    const firstDelete = deferred<undefined>();
    mocks.deletePolicyGrant.mockReturnValueOnce(firstDelete.promise);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(mocks.deletePolicyGrant).toHaveBeenCalledTimes(1),
    );

    // Write permission goes away while that request is on the wire, which
    // remounts the section under a new key and unmounts this instance.
    mocks.agent.permissions = {
      read: true,
      write: false,
      authorize: false,
      transfer: false,
    };
    rerenderPage();

    firstDelete.resolve(undefined);

    // The cached ceiling and the delegable candidates are dropped, because the
    // resolved request may or may not have committed.
    await waitFor(() =>
      expect(client.getQueryState(delegableKey)?.data).toBeUndefined(),
    );
    // The second removal was never issued after the context went away.
    expect(mocks.deletePolicyGrant).toHaveBeenCalledTimes(1);
    expect(mocks.deletePolicyGrant.mock.calls[0]?.[0]).toEqual({
      agentPolicyGrantIDForm: {
        agentId: "agent_example",
        grantId: "grant_one",
      },
    });
    expect(mocks.createPolicyGrant).not.toHaveBeenCalled();

    // The read-only editor that replaced it shows what the server says, not the
    // abandoned draft, and no error was pushed into the unmounted instance.
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Remove mcp:connect" }),
      ).toBeTruthy(),
    );
    expect(
      screen.getByRole("button", { name: "Remove skill:read" }),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Reload permissions" }),
    ).toBeNull();
  });

  it("refetches rather than serving the cleared ceiling from cache", async () => {
    const { client, rerenderPage } = setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Remove mcp:connect" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove skill:read" }));

    const firstDelete = deferred<undefined>();
    mocks.deletePolicyGrant.mockReturnValueOnce(firstDelete.promise);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(mocks.deletePolicyGrant).toHaveBeenCalledTimes(1),
    );
    const readsBeforeAbandon = mocks.listPolicyGrants.mock.calls.length;

    mocks.agent.permissions = {
      read: true,
      write: false,
      authorize: false,
      transfer: false,
    };
    rerenderPage();
    // Only one grant actually went away on the server.
    mocks.listPolicyGrants.mockResolvedValue([storedGrants[1]]);
    firstDelete.resolve(undefined);

    // The still-active observer is made to read again rather than keep the
    // pre-save list it was showing.
    await waitFor(() =>
      expect(mocks.listPolicyGrants.mock.calls.length).toBeGreaterThan(
        readsBeforeAbandon,
      ),
    );
    await waitFor(() => expect(client.getQueryData(policyKey)).toHaveLength(1));
    expect(
      screen.queryByRole("button", { name: "Remove mcp:connect" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "Remove skill:read" }),
    ).toBeTruthy();
  });

  it("does not touch the caches when the context goes away before any request", async () => {
    const { client, rerenderPage } = setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Remove mcp:connect" }),
    );
    client.setQueryData(delegableKey, []);

    // Hang the pre-save confirming read, so the save has issued no write yet.
    const read = deferred<unknown[]>();
    mocks.listPolicyGrants.mockReturnValueOnce(read.promise);
    fireEvent.click(screen.getByRole("button", { name: "Save permissions" }));

    mocks.agent.permissions = {
      read: true,
      write: false,
      authorize: false,
      transfer: false,
    };
    rerenderPage();
    read.resolve(storedGrants);

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Remove mcp:connect" }),
      ).toBeTruthy(),
    );
    expect(mocks.deletePolicyGrant).not.toHaveBeenCalled();
    expect(mocks.createPolicyGrant).not.toHaveBeenCalled();
    // Nothing was written, so the primed candidate set is left alone.
    expect(client.getQueryData(delegableKey)).toEqual([]);
  });
});

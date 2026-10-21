import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { invalidateAgentPolicy } from "../agent-policy-grants";
import { ProvisionWizard } from "./ProvisionWizard";

const mocks = vi.hoisted(() => ({
  createAgent: vi.fn(),
  createKey: vi.fn(),
  listKeys: vi.fn(),
  listDelegableGrants: vi.fn(),
  mcpServers: vi.fn(),
  toolsets: vi.fn(),
  fetch: vi.fn(),
  platforms: vi.fn(),
  registerIssuer: vi.fn(),
  admitSubject: vi.fn(),
  projectId: "project_one",
  canFederate: true,
  renameAgent: vi.fn(),
  createGrant: vi.fn(),
  deleteGrant: vi.fn(),
  agentList: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    name: "Example Org",
    slug: "example",
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
  useProject: () => ({
    id: mocks.projectId,
    name: "Project one",
    slug: "project-one",
  }),
  useSession: () => ({
    user: { id: "user_owner", displayName: "Example Owner", email: "o@e.test" },
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    agents: {
      create: mocks.createAgent,
      listDelegableGrants: mocks.listDelegableGrants,
    },
    keys: { create: mocks.createKey, list: mocks.listKeys },
  }),
  // The server list links out to a server's settings, and routes read the
  // slugs to build that href.
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));
// The credential step offers workload identity, which reads the catalog the
// server ships and the caller's permission to trust a platform.
vi.mock("@gram/client/react-query/workloadPlatforms.js", () => ({
  useWorkloadPlatforms: () => ({
    data: { platforms: mocks.platforms() },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: () => mocks.canFederate,
    hasAnyScope: () => mocks.canFederate,
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({ data: { issuers: [], admissions: [] } }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data: { items: [] },
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({
    mutateAsync: mocks.registerIssuer,
  }),
}));
vi.mock("@gram/client/react-query/admitWorkloadSubject.js", () => ({
  useAdmitWorkloadSubjectMutation: () => ({ mutateAsync: mocks.admitSubject }),
}));
// Editing writes through these three: the name, and the grants that decide
// which servers the agent reaches.
vi.mock("@gram/client/react-query/renameAgent.js", () => ({
  useRenameAgentMutation: () => ({ mutateAsync: mocks.renameAgent }),
}));
vi.mock("@gram/client/react-query/agentsCreatePolicyGrant.js", () => ({
  useAgentsCreatePolicyGrantMutation: () => ({
    mutateAsync: mocks.createGrant,
  }),
}));
vi.mock("@gram/client/react-query/agentsDeletePolicyGrant.js", () => ({
  useAgentsDeletePolicyGrantMutation: () => ({
    mutateAsync: mocks.deleteGrant,
  }),
}));
// Only read to keep a prefilled name from colliding with one in use.
vi.mock("@gram/client/react-query/agents.js", () => ({
  useAgents: () => ({ data: { result: { items: mocks.agentList() } } }),
}));
vi.mock("@gram/client/react-query/listMcpServersForOrg.js", () => ({
  useListMcpServersForOrg: (_r: unknown, _s: unknown, options: object) =>
    useQuery({
      queryKey: ["org-mcp-servers"],
      queryFn: () => mocks.mcpServers(),
      ...options,
    }),
}));
vi.mock("@gram/client/react-query/listToolsetsForOrg.js", () => ({
  useListToolsetsForOrg: (_r: unknown, _s: unknown, options: object) =>
    useQuery({
      queryKey: ["org-toolsets"],
      queryFn: () => mocks.toolsets(),
      ...options,
    }),
}));
// A headless device's setup creates its bootstrap token on the provision
// step, and its verify step polls the agent's tokens.
vi.mock("@gram/client/react-query/createWorkloadDeviceJoinToken.js", () => ({
  useCreateWorkloadDeviceJoinTokenMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));
vi.mock(
  "@gram/client/react-query/createWorkloadDeviceInstallCommand.js",
  () => ({
    useCreateWorkloadDeviceInstallCommandMutation: () => ({
      mutateAsync: vi.fn(),
    }),
  }),
);
vi.mock("@gram/client/react-query/workloadDeviceJoinTokens.js", () => ({
  useWorkloadDeviceJoinTokens: () => ({ data: undefined, isPending: true }),
}));
vi.mock("../agent-policy-grants", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../agent-policy-grants")>()),
  invalidateAgentPolicy: vi.fn(),
}));

const grant = {
  effect: "allow",
  scope: "mcp:connect",
  selector: {
    resourceKind: "mcp",
    resourceId: "toolset_one",
    projectId: "project_one",
  },
};

function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onDone = vi.fn();
  const view = render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <ProvisionWizard
          onDone={(id) => {
            onDone(id);
          }}
          // The real cadence is five seconds, which the verify test would have
          // to either outwait or race.
          verifyPollMs={20}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  return { ...view, onDone };
}

async function reachCredentialStep() {
  setup();
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Release Bot" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  return screen.findByRole("button", { name: "Create agent" });
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.projectId = "project_one";
  mocks.canFederate = true;
  mocks.platforms.mockReturnValue([]);
  mocks.agentList.mockReturnValue([]);
  mocks.renameAgent.mockResolvedValue({});
  mocks.createGrant.mockResolvedValue({});
  mocks.deleteGrant.mockResolvedValue({});
  mocks.registerIssuer.mockResolvedValue({});
  mocks.admitSubject.mockResolvedValue({});
  vi.stubGlobal("fetch", mocks.fetch);
  mocks.mcpServers.mockResolvedValue({
    mcpServers: [
      {
        id: "server_one",
        name: "GitHub",
        slug: "github",
        projectId: "project_one",
        toolsetId: "toolset_one",
        visibility: "private",
        networkAccessMode: "dual",
        createdAt: new Date(),
        updatedAt: new Date(),
      },
    ],
  });
  mocks.toolsets.mockResolvedValue({ toolsets: [] });
  mocks.createAgent.mockResolvedValue({ id: "agent_new", name: "Release Bot" });
  mocks.listDelegableGrants.mockResolvedValue([grant]);
  mocks.createKey.mockResolvedValue({ id: "key_new", key: "gram_secret" });
  mocks.listKeys.mockResolvedValue({ keys: [] });
  mocks.fetch.mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ code: "setup_code" }),
  });
});

describe("Provisioning a new agent", () => {
  it("will not move on without a name or a server", async () => {
    setup();
    expect(
      (screen.getByRole("button", { name: "Continue" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(screen.getByText("Name this agent.")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Release Bot" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByRole("heading", { name: "Server selection" });
    expect(
      (screen.getByRole("button", { name: "Continue" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(screen.getByText("Select at least one MCP server.")).toBeTruthy();
  });

  it("creates the agent, issues its first key and mints one setup command", async () => {
    fireEvent.click(await reachCredentialStep());

    await waitFor(() => expect(mocks.createKey).toHaveBeenCalledTimes(1));
    // The policy is the servers the user chose, not a wildcard the key would
    // then have to be narrowed back down from.
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "toolset_one",
          projectId: "project_one",
        },
      },
    ]);
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.projectId,
    ).toBe("project_one");
    expect(mocks.createKey.mock.calls[0]?.[0]?.createKeyForm?.agentId).toBe(
      "agent_new",
    );

    // The command renders in a syntax-highlighted block, so it reaches the
    // DOM as a run of token elements rather than one string. Read the block.
    const setupCommand = await screen.findByLabelText("setup command");
    await waitFor(() =>
      expect(setupCommand.textContent?.replace(/\s+/g, " ")).toMatch(
        /curl -fsSL .*\/agent-mcp\/install\/setup_code/,
      ),
    );
    expect(mocks.fetch).toHaveBeenCalledTimes(1);
  });

  it("keeps the agent when the key fails, and says which part did not happen", async () => {
    mocks.createKey.mockRejectedValue(new Error("key refused"));
    fireEvent.click(await reachCredentialStep());

    expect((await screen.findByRole("alert")).textContent).toContain(
      "key refused",
    );
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
  });

  it("does not count the dashboard's own use of the key as the first call", async () => {
    // Minting the setup command authenticates with the key, so the key's
    // access time moves before any runtime has called.
    mocks.listKeys.mockResolvedValue({
      keys: [{ id: "key_new", lastAccessedAt: new Date(Date.now() - 60_000) }],
    });
    fireEvent.click(await reachCredentialStep());
    fireEvent.click(
      await screen.findByRole("button", { name: "Continue to verification" }),
    );

    expect(await screen.findByText("Waiting for the first call")).toBeTruthy();

    mocks.listKeys.mockResolvedValue({
      keys: [{ id: "key_new", lastAccessedAt: new Date(Date.now() + 60_000) }],
    });
    expect(await screen.findByText("Connected")).toBeTruthy();
  });
});

const platform = {
  key: "claude-tag",
  displayName: "Claude Tag",
  description: "Tokens issued by the platform itself.",
  icon: "",
  enabled: true,
  issuer: { value: "https://issuer.example", visibility: "hidden" },
  jwksUri: { value: "https://issuer.example/jwks", visibility: "hidden" },
  variables: [
    {
      key: "org_id",
      tier: "rule",
      label: "Anthropic organization ID",
      help: "",
      placeholder: "",
      pattern: "[A-Za-z0-9-]+",
      patternMessage: "",
    },
  ],
  subject: {
    template: "spiffe://example/org/{org_id}/agent/",
    wildcard: true,
  },
};

async function reachPlatformSetup() {
  mocks.platforms.mockReturnValue([platform]);
  setup();
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Release Bot" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  fireEvent.click(await screen.findByRole("radio", { name: /Workload/ }));
  fireEvent.click(await screen.findByRole("radio", { name: /Claude Tag/ }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  // The platform's own steps are steps of this wizard, so its first one is
  // simply the next screen.
  return screen.findByRole("heading", {
    name: "Anthropic organization",
  });
}

describe("Federating a platform's own tokens", () => {
  it("keeps the credential to API keys without permission to trust a platform", async () => {
    mocks.canFederate = false;
    mocks.platforms.mockReturnValue([platform]);
    await reachCredentialStep();

    // The choice is on screen but cannot be taken, so the platforms behind it
    // are never offered either.
    const workload = screen.getByRole("radio", { name: /Workload/ });
    expect(workload.getAttribute("data-disabled")).not.toBeNull();
    expect(screen.queryByRole("radio", { name: /Claude Tag/ })).toBeNull();
  });

  it("creates the agent, trusts the platform and admits the subject to it", async () => {
    await reachPlatformSetup();

    fireEvent.change(screen.getByLabelText("Anthropic organization ID"), {
      target: { value: "org-123" },
    });
    // Saving asked only for the agent, which this wizard answers, so it is
    // not a step of its own: it joined the step that collects.
    expect(
      screen.queryByRole("heading", { name: "Save the access rule" }),
    ).toBeNull();
    expect(screen.queryByLabelText("Agent")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Connect Claude Tag" }));

    await waitFor(() => expect(mocks.admitSubject).toHaveBeenCalledTimes(1));
    // Nothing is created until the setup's own create step runs, and then the
    // agent comes first: the access rule names it.
    expect(mocks.createAgent).toHaveBeenCalledTimes(1);
    expect(mocks.createKey).not.toHaveBeenCalled();
    expect(
      mocks.admitSubject.mock.calls[0]?.[0]?.request?.admitWorkloadSubjectForm,
    ).toEqual({
      issuer: "https://issuer.example",
      subject: "spiffe://example/org/org-123/agent/*",
      matchKind: "wildcard",
      agentId: "agent_new",
      tags: [],
    });
    expect(
      mocks.registerIssuer.mock.calls[0]?.[0]?.request
        ?.registerWorkloadIssuerForm?.issuer,
    ).toBe("https://issuer.example");
  });
});

describe("Enrolling a headless device", () => {
  async function reachDeviceChoice() {
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Build Box" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("radio", { name: /Workload/ }));
    fireEvent.click(
      await screen.findByRole("radio", { name: /Headless device/ }),
    );
  }

  it("grants device sync and hooks for the chosen project with the agent", async () => {
    await reachDeviceChoice();
    expect(screen.getByLabelText("Hooks project")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));

    await screen.findByRole("heading", { name: "Enroll the box" });
    expect(mocks.createKey).not.toHaveBeenCalled();
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants,
    ).toEqual([
      grant,
      {
        effect: "allow",
        scope: "org:device_agent_sync",
        selector: { resourceKind: "org", resourceId: "org_example" },
      },
      {
        effect: "allow",
        scope: "org:hooks_ingest",
        selector: {
          resourceKind: "org",
          resourceId: "org_example",
          projectId: "project_one",
        },
      },
    ]);
    // With the install command switched off, setting up by hand is open.
    expect(screen.getByText("Set up manually")).toBeTruthy();
  });

  it("enrolls without hooks when they are not wanted, and says what that means", async () => {
    await reachDeviceChoice();
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Accept hooks from this project" }),
    );
    expect(screen.getByText(/hook events are refused/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));

    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    const policy: { scope: string }[] =
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants ?? [];
    const scopes = policy.map((entry) => entry.scope);
    expect(scopes).toEqual(["mcp:connect", "org:device_agent_sync"]);
  });

  it("is not offered without permission to manage workloads", async () => {
    mocks.canFederate = false;
    await reachCredentialStep();
    expect(screen.queryByRole("radio", { name: /Headless device/ })).toBeNull();
  });
});

describe("Agent scope", () => {
  it("defaults to the active project and binds the agent to it", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Project bound" },
    });
    // Checked before the step is left behind: the control only exists here.
    expect(
      screen.getByRole("radio", { name: /Project/ }).getAttribute("data-state"),
    ).toBe("checked");
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );

    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    const form = mocks.createAgent.mock.calls[0]![0].createAgentForm;
    expect(form.projectId).toBe("project_one");
  });

  it("sends no project binding when organization scope is chosen", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Org wide" },
    });
    fireEvent.click(screen.getByRole("radio", { name: /Organization/ }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );

    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    const form = mocks.createAgent.mock.calls[0]![0].createAgentForm;
    // Omitted rather than blank: the server reads a missing binding as
    // organization-wide.
    expect("projectId" in form).toBe(false);
  });

  it("withholds project scope until a project resolves", () => {
    // useProject yields an empty id before a project resolves. Binding to it
    // would send an empty id, which the server reads as "omitted" — silently
    // creating an organization-wide agent after the user asked for a project
    // one.
    mocks.projectId = "";
    setup();

    const projectOption = screen.getByRole("radio", { name: /Project/ });
    expect(projectOption.getAttribute("data-disabled")).not.toBeNull();
    expect(
      screen
        .getByRole("radio", { name: /Organization/ })
        .getAttribute("data-state"),
    ).toBe("checked");
  });
});

describe("Editing an agent that already exists", () => {
  const agent = {
    id: "agent_one",
    name: "Release Bot",
    lifecycle: "active",
    permissions: { write: true, authorize: true },
  } as never;

  const grants = [
    {
      id: "grant_one",
      effect: "allow",
      scope: "mcp:connect",
      selector: {
        resourceKind: "mcp",
        resourceId: "toolset_one",
        projectId: "project_one",
      },
      createdAt: new Date(),
      updatedAt: new Date(),
    },
  ] as never;

  function editing(over: { grants?: unknown } = {}) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const onDone = vi.fn();
    const view = render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <ProvisionWizard
            agent={agent}
            editing
            grants={(over.grants ?? grants) as never}
            onDone={(id) => {
              onDone(id);
            }}
            verifyPollMs={20}
          />
        </QueryClientProvider>
      </MemoryRouter>,
    );
    return { ...view, onDone };
  }

  it("opens on the agent's own name, which it lets you change", () => {
    editing();
    const field = screen.getByLabelText("Name") as HTMLInputElement;
    expect(field.value).toBe("Release Bot");
    expect(field.disabled).toBe(false);
  });

  it("ticks off the credential steps, which editing does not revisit", () => {
    editing();
    // Marked done rather than ahead: an agent that exists has settled what
    // it signs in with. The state is asserted, not merely the label: a cell
    // carries its state in its own accessible name.
    const credential = screen.getByRole("button", { name: /Credential/ });
    expect(credential.textContent).toContain("Done");
    expect(credential.textContent).not.toContain("Current");
    // The step the reader is on keeps its place even though everything from
    // the credential on is settled.
    const current = screen.getByRole("button", { name: /Name & scope/ });
    expect(current.getAttribute("aria-current")).toBe("step");
    expect(current.textContent).toContain("Current");
  });

  it("marks a settled step the reader jumped to as the one they are on", () => {
    editing();
    // Settled steps are still destinations, and a reader standing on one has
    // to be able to see where they are.
    fireEvent.click(screen.getByRole("button", { name: /Credential/ }));
    const credential = screen.getByRole("button", { name: /Credential/ });
    expect(credential.getAttribute("aria-current")).toBe("step");
    expect(credential.textContent).toContain("Current");
  });

  it("checks the servers the agent already reaches", async () => {
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const server = await screen.findByRole("checkbox", { name: "GitHub" });
    await waitFor(() =>
      expect(server.getAttribute("data-state")).toBe("checked"),
    );
  });

  it("saves a rename and leaves without touching the grants", async () => {
    const { onDone } = editing();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Renamed Bot" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

    await waitFor(() => expect(mocks.renameAgent).toHaveBeenCalledTimes(1));
    expect(
      mocks.renameAgent.mock.calls[0]?.[0]?.request?.renameAgentForm,
    ).toEqual({ id: "agent_one", name: "Renamed Bot" });
    expect(mocks.createGrant).not.toHaveBeenCalled();
    expect(mocks.deleteGrant).not.toHaveBeenCalled();
    await waitFor(() => expect(onDone).toHaveBeenCalledWith(undefined));
  });

  it("writes nothing when neither the name nor the servers changed", async () => {
    const { onDone } = editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

    await waitFor(() => expect(onDone).toHaveBeenCalledWith(undefined));
    expect(mocks.renameAgent).not.toHaveBeenCalled();
    expect(mocks.createGrant).not.toHaveBeenCalled();
    expect(mocks.deleteGrant).not.toHaveBeenCalled();
  });

  it("withdraws the grant for a server that was unchecked", async () => {
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const server = await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(server);
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

    await waitFor(() => expect(mocks.deleteGrant).toHaveBeenCalledTimes(1));
    expect(
      mocks.deleteGrant.mock.calls[0]?.[0]?.request?.agentPolicyGrantIDForm,
    ).toEqual({ agentId: "agent_one", grantId: "grant_one" });
    expect(mocks.createGrant).not.toHaveBeenCalled();
  });

  it("grants a server that was not reachable before", async () => {
    editing({ grants: [] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const server = await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(server);
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

    await waitFor(() => expect(mocks.createGrant).toHaveBeenCalledTimes(1));
    expect(
      mocks.createGrant.mock.calls[0]?.[0]?.request?.createAgentPolicyGrantForm,
    ).toEqual({
      agentId: "agent_one",
      effect: "allow",
      scope: "mcp:connect",
      selector: {
        resourceKind: "mcp",
        resourceId: "toolset_one",
        projectId: "project_one",
      },
    });
    expect(mocks.deleteGrant).not.toHaveBeenCalled();
  });

  it("creates no agent: this one already exists", async () => {
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

    await waitFor(() => expect(mocks.renameAgent).not.toHaveBeenCalled());
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it("says so when a wildcard grant already reaches every server", async () => {
    // The list leaves the wildcard out on purpose, and a save would not
    // remove it, so an unticked list is not this agent's reach.
    editing({
      grants: [
        {
          id: "grant_all",
          effect: "allow",
          scope: "mcp:connect",
          selector: { resourceKind: "mcp", resourceId: "*" },
          createdAt: new Date(),
          updatedAt: new Date(),
        },
      ],
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("checkbox", { name: "GitHub" });
    expect(
      screen.getByText(/reaches every MCP server through a wildcard grant/),
    ).toBeTruthy();
  });

  it("will not save while the server list has not loaded", async () => {
    // An unread list leaves the selection empty, which is indistinguishable
    // from "no servers chosen" — and saving that withdraws every grant.
    mocks.mcpServers.mockRejectedValue(new Error("server list refused"));
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const save = (await screen.findByRole("button", {
      name: "Save changes and exit",
    })) as HTMLButtonElement;
    await waitFor(() => expect(save.disabled).toBe(true));
    fireEvent.click(save);
    expect(mocks.deleteGrant).not.toHaveBeenCalled();
    expect(mocks.createGrant).not.toHaveBeenCalled();
  });

  it("drops the agent's cached policy once the grants are written", async () => {
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

    await waitFor(() => expect(mocks.deleteGrant).toHaveBeenCalledTimes(1));
    // Saving leads to the agent's own page, which reads its servers from
    // these caches.
    expect(vi.mocked(invalidateAgentPolicy)).toHaveBeenCalledWith(
      expect.anything(),
      "org_example",
      "user_owner",
      "agent_one",
    );
  });

  it("says why a save failed and stays on the step", async () => {
    mocks.deleteGrant.mockRejectedValueOnce(new Error("Grant is locked"));
    const { onDone } = editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const server = await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(server);
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

    expect(await screen.findByText("Grant is locked")).toBeTruthy();
    expect(onDone).not.toHaveBeenCalled();
  });
});

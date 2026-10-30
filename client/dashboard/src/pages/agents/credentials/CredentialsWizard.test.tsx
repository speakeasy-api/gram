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
import { CredentialsWizard } from "./CredentialsWizard";

const mocks = vi.hoisted(() => ({
  createAgent: vi.fn(),
  createKey: vi.fn(),
  listKeys: vi.fn(),
  listDelegableGrants: vi.fn(),
  checkPolicyGrants: vi.fn(),
  mcpServers: vi.fn(),
  toolsets: vi.fn(),
  fetch: vi.fn(),
  platforms: vi.fn(),
  tokenEndpoints: vi.fn(),
  platformsStatus: "success" as "success" | "pending" | "error",
  registerIssuer: vi.fn(),
  admitSubject: vi.fn(),
  withdrawSubject: vi.fn(),
  workloadIssuers: vi.fn(),
  workloadAdmissions: vi.fn(),
  projectId: "project_one",
  canFederate: true,
  canReadWorkloads: true,
  canIngestHooks: false,
  renameAgent: vi.fn(),
  createGrant: vi.fn(),
  deleteGrant: vi.fn(),
  agentList: vi.fn(),
  sessions: vi.fn(),
  sessionClients: vi.fn(),
  bindings: vi.fn(),
  attach: vi.fn(),
  plugins: vi.fn(),
  getPlugin: vi.fn(),
  listPolicyGrants: vi.fn(),
}));

vi.mock("@gram/client/react-query/mcpServerAuthClientChoices.js", () => ({
  useMcpServerAuthClientChoices: () => ({
    isError: false,
    isSuccess: true,
    data: { choices: [] },
  }),
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("@gram/client/react-query/remoteSessionsAttachBinding.js", () => ({
  useRemoteSessionsAttachBindingMutation: () => ({ mutateAsync: mocks.attach }),
}));
vi.mock("@gram/client/react-query/remoteSessionsDetachBinding.js", () => ({
  useRemoteSessionsDetachBindingMutation: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  firstPartyConnectUrl: (url: string | undefined) =>
    url
      ? `${new URL(url, "https://speakeasy.example")}/connect/first-party`
      : undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    name: "Example Org",
    slug: "example",
    // Two, so a wildcard that names no project can be told apart from one
    // scoped to the project the agent is labelled with.
    projects: [
      { id: "project_one", name: "Project one", slug: "project-one" },
      { id: "project_two", name: "Project two", slug: "project-two" },
    ],
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
      listPolicyGrants: mocks.listPolicyGrants,
      checkPolicyGrants: mocks.checkPolicyGrants,
    },
    keys: { create: mocks.createKey, list: mocks.listKeys },
    plugins: { getPlugin: mocks.getPlugin },
    authClients: { listMcpServerChoices: async () => ({ choices: [] }) },
    // A person's own connections, read once per project before the agent
    // exists and as the agent's candidates once it does.
    remoteSessions: {
      list: async (request: object) => [
        { result: { items: await mocks.sessions(request) } },
      ],
      listBindings: async () => ({ items: await mocks.bindings() }),
      attachBinding: mocks.attach,
    },
    remoteSessionClients: {
      list: async (request: object) => [
        { result: { items: await mocks.sessionClients(request) } },
      ],
    },
  }),
  // The server list links out to a server's settings, and routes read the
  // slugs to build that href.
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));
// The credential step offers workload identity, which reads the catalog the
// server ships and the caller's permission to trust a platform.
vi.mock("@gram/client/react-query/workloadPlatforms.js", () => ({
  // A disabled query never fetches and, like react-query, stays pending.
  useWorkloadPlatforms: (
    _request: unknown,
    _security: unknown,
    options?: { enabled?: boolean },
  ) => {
    if (options?.enabled === false) {
      return {
        data: undefined,
        isPending: true,
        isError: false,
        refetch: vi.fn(),
      };
    }
    return {
      data:
        mocks.platformsStatus === "success"
          ? { platforms: mocks.platforms() }
          : undefined,
      isPending: mocks.platformsStatus === "pending",
      isError: mocks.platformsStatus === "error",
      refetch: vi.fn(),
    };
  },
}));
// Reading the workload catalog and sending hook events are their own
// permissions; everything else this flow checks follows canFederate.
function hasScope(scope: string): boolean {
  if (scope === "workload:read") return mocks.canReadWorkloads;
  if (scope === "org:hooks_ingest") return mocks.canIngestHooks;
  return mocks.canFederate;
}
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: (scopes: string[]) => scopes.every(hasScope),
    hasAnyScope: (scopes: string[]) => scopes.some(hasScope),
    hasScope,
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({
    data: {
      issuers: mocks.workloadIssuers(),
      admissions: mocks.workloadAdmissions(),
    },
  }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data: { items: mocks.tokenEndpoints() },
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
vi.mock("@gram/client/react-query/withdrawWorkloadSubject.js", () => ({
  useWithdrawWorkloadSubjectMutation: () => ({
    mutateAsync: mocks.withdrawSubject,
  }),
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
// The GitHub Actions setup lists the plugins the job will receive.
vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: () => ({
    data: { plugins: mocks.plugins() },
    isPending: false,
    isError: false,
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
vi.mock("../agent-policy-grants", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../agent-policy-grants")>()),
  invalidateAgentPolicy: vi.fn(),
}));
// Highlighting is asynchronous and splits code into token elements; the
// snippet's text is what these tests read.
vi.mock("./CodeBlock", () => ({
  CodeBlock: ({ value, label }: { value: string; label: string }) => (
    <pre aria-label={label}>{value}</pre>
  ),
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

function setup(initialPlatformKey?: string, initialEntry = "/") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onDone = vi.fn();
  const view = render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={client}>
        <CredentialsWizard
          initialPlatformKey={initialPlatformKey}
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

async function continueFromAuthentication() {
  await screen.findByRole("heading", { name: "MCP Authentication" });
  const next = screen.getByRole("button", { name: "Continue" });
  await waitFor(() => expect(next.hasAttribute("disabled")).toBe(false));
  fireEvent.click(next);
}

async function reachCredentialStep() {
  setup();
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Release Bot" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await continueFromAuthentication();
  return screen.findByRole("button", { name: "Create agent" });
}

type CheckRequest = {
  checkAgentPolicyGrantsForm: {
    policyGrants: { selector: { resourceId: string } }[];
  };
};

/**
 * Answers a policy check grant by grant: refused where `refuse` names the
 * grant's resource ("*" for All servers), allowed everywhere else.
 */
function answerChecks(refuse: Record<string, "caller" | "owner"> = {}) {
  return (request: CheckRequest) =>
    Promise.resolve({
      results: request.checkAgentPolicyGrantsForm.policyGrants.map((grant) => {
        const limitedBy = refuse[grant.selector.resourceId];
        return limitedBy === undefined
          ? { allowed: true }
          : { allowed: false, limitedBy };
      }),
    });
}

/**
 * The "All servers" box, once the check of whether it can be saved has
 * answered: until then it cannot be ticked.
 */
async function enabledAllServers() {
  const box = await screen.findByRole("checkbox", { name: "All servers" });
  await waitFor(() => expect(box.hasAttribute("disabled")).toBe(false));
  return box;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
beforeEach(() => {
  vi.clearAllMocks();
  mocks.projectId = "project_one";
  mocks.canFederate = true;
  mocks.canReadWorkloads = true;
  mocks.canIngestHooks = false;
  mocks.platformsStatus = "success";
  mocks.platforms.mockReturnValue([]);
  mocks.tokenEndpoints.mockReturnValue([]);
  mocks.agentList.mockReturnValue([]);
  mocks.renameAgent.mockResolvedValue({});
  mocks.createGrant.mockResolvedValue({});
  mocks.deleteGrant.mockResolvedValue({});
  mocks.registerIssuer.mockResolvedValue({});
  mocks.admitSubject.mockResolvedValue({});
  mocks.withdrawSubject.mockResolvedValue({});
  mocks.workloadIssuers.mockReturnValue([]);
  mocks.workloadAdmissions.mockReturnValue([]);
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
  mocks.sessions.mockResolvedValue([]);
  mocks.sessionClients.mockResolvedValue([]);
  mocks.bindings.mockResolvedValue([]);
  mocks.attach.mockResolvedValue({});
  mocks.plugins.mockReturnValue([]);
  mocks.getPlugin.mockReset();
  mocks.listPolicyGrants.mockResolvedValue([]);
  mocks.createAgent.mockResolvedValue({ id: "agent_new", name: "Release Bot" });
  mocks.listDelegableGrants.mockResolvedValue([grant]);
  mocks.checkPolicyGrants.mockImplementation(answerChecks());
  mocks.createKey.mockResolvedValue({ id: "key_new", key: "gram_secret" });
  mocks.listKeys.mockResolvedValue({ keys: [] });
  mocks.fetch.mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ code: "setup_code" }),
  });
});

describe("Provisioning a new agent", () => {
  it("starts the draft over when reloaded past its first step", async () => {
    // Nothing of a new agent's draft survives a reload, so the install step
    // has no name to create an agent with and no key to show.
    setup(undefined, "/?step=3");

    expect(await screen.findByLabelText("Name")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Issue a key" })).toBeNull();
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it("starts the draft over from a link past the last step", async () => {
    setup(undefined, "/?step=9");

    expect(await screen.findByLabelText("Name")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: /Done|Skip for now/ }),
    ).toBeNull();
  });

  it("starts a named platform's draft over at its servers", async () => {
    mocks.platforms.mockReturnValue([platform]);
    setup(platform.key, "/?step=3");

    expect(
      await screen.findByRole("heading", { name: "MCP Selection" }),
    ).toBeTruthy();
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

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

    await screen.findByRole("heading", { name: "MCP Selection" });
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

  it("gives a new agent hook ingestion in its project, and asks its first key for it", async () => {
    mocks.canIngestHooks = true;
    const hooksGrant = {
      effect: "allow",
      scope: "org:hooks_ingest",
      selector: { resourceKind: "org", resourceId: "org_example" },
    };
    // Discovery that names servers answers with the server grant; discovery
    // that names none answers with the organization grants.
    mocks.listDelegableGrants.mockImplementation(
      async (request: { toolsetIds?: string[] }) =>
        request.toolsetIds ? [grant] : [hooksGrant],
    );
    fireEvent.click(await reachCredentialStep());

    await waitFor(() => expect(mocks.createKey).toHaveBeenCalledTimes(1));
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants,
    ).toContainEqual({
      effect: "allow",
      scope: "org:hooks_ingest",
      selector: {
        resourceKind: "org",
        resourceId: "org_example",
        projectId: "project_one",
      },
    });
    // The key narrows the hooks grant to the agent's project.
    expect(
      mocks.createKey.mock.calls[0]?.[0]?.createKeyForm?.requestedGrants,
    ).toContainEqual({
      ...hooksGrant,
      selector: { ...hooksGrant.selector, projectId: "project_one" },
    });
  });

  it("does not give hook ingestion when its creator cannot grant it", async () => {
    fireEvent.click(await reachCredentialStep());

    await waitFor(() => expect(mocks.createKey).toHaveBeenCalledTimes(1));
    const scopes = (
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants ?? []
    ).map((policy: { scope: string }) => policy.scope);
    expect(scopes).not.toContain("org:hooks_ingest");
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
  await continueFromAuthentication();
  // A preset sits beside the API key, so it is one choice, not two.
  fireEvent.click(await screen.findByRole("radio", { name: /Claude Tag/ }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  // The platform's own steps are steps of this wizard, so its first one is
  // simply the next screen.
  return screen.findByRole("heading", {
    name: "Anthropic organization",
  });
}

describe("Federating a platform's own tokens", () => {
  it.each([
    [
      "unknown issuer visibility",
      { ...platform, issuer: { ...platform.issuer, visibility: "future" } },
    ],
    [
      "prototype issuer visibility",
      {
        ...platform,
        issuer: { ...platform.issuer, visibility: "constructor" },
      },
    ],
    [
      "unknown key visibility",
      { ...platform, jwksUri: { ...platform.jwksUri, visibility: "future" } },
    ],
    [
      "unknown variable tier",
      {
        ...platform,
        variables: platform.variables.map((variable) => ({
          ...variable,
          tier: "future",
        })),
      },
    ],
    [
      "prototype variable tier",
      {
        ...platform,
        variables: platform.variables.map((variable) => ({
          ...variable,
          tier: "toString",
        })),
      },
    ],
    ["disabled platform", { ...platform, enabled: false }],
    ["unknown platform", { ...platform, key: "constructor" }],
  ])(
    "keeps %s visible without allowing preselected setup",
    async (_case, unavailable) => {
      mocks.platforms.mockReturnValue([unavailable]);
      setup(unavailable.key);
      fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));

      await continueFromAuthentication();
      expect(await screen.findByText("Unavailable")).toBeTruthy();
      expect(screen.getByText(platform.description)).toBeTruthy();
      expect(screen.queryByRole("radio", { name: /Claude Tag/ })).toBeNull();
      const next = screen.getByRole("button", { name: "Continue" });
      expect(next.hasAttribute("disabled")).toBe(true);
      fireEvent.click(next);
      expect(screen.queryByLabelText("Anthropic organization ID")).toBeNull();
      expect(mocks.createAgent).not.toHaveBeenCalled();
      expect(mocks.registerIssuer).not.toHaveBeenCalled();
      expect(mocks.admitSubject).not.toHaveBeenCalled();

      // The independent custom flow remains an available recovery path,
      // under the advanced options.
      fireEvent.click(screen.getByRole("button", { name: /^Advanced:/ }));
      fireEvent.click(
        screen.getByRole("radio", { name: /Custom OIDC issuer/ }),
      );
      expect(
        screen
          .getByRole("button", { name: "Continue" })
          .hasAttribute("disabled"),
      ).toBe(false);
    },
  );

  it("keeps the credential to API keys without permission to trust a platform", async () => {
    mocks.canFederate = false;
    mocks.platforms.mockReturnValue([platform]);
    await reachCredentialStep();

    // The presets and the custom issuer stay on screen, so the reader learns
    // they exist, but none of them can be taken.
    const preset = await screen.findByRole("radio", { name: /Claude Tag/ });
    expect(preset.getAttribute("data-disabled")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^Advanced:/ }));
    const custom = screen.getByRole("radio", { name: /Custom OIDC issuer/ });
    expect(custom.getAttribute("data-disabled")).not.toBeNull();
    const apiKey = screen.getByRole("radio", { name: /API key/ });
    expect(apiKey.getAttribute("data-disabled")).toBeNull();
    expect(
      screen.getByText(/need permission to manage workload identities/),
    ).toBeTruthy();
  });

  it("offers the API key alone, with no error, to someone who cannot read the catalog", async () => {
    // Reading the catalog is refused without workload access, so the request
    // is never made. Before the presets moved up front this went unnoticed;
    // shown as a load failure, it read as a broken page.
    mocks.canFederate = false;
    mocks.canReadWorkloads = false;
    mocks.platforms.mockReturnValue([platform]);
    await reachCredentialStep();

    expect(screen.queryByRole("radio", { name: /Claude Tag/ })).toBeNull();
    expect(screen.queryByText(/could not be loaded/)).toBeNull();
    expect(screen.queryByText(/Loading platforms/)).toBeNull();
    const apiKey = screen.getByRole("radio", { name: /API key/ });
    expect(apiKey.getAttribute("data-disabled")).toBeNull();
    expect(
      screen.getByText(/need permission to manage workload identities/),
    ).toBeTruthy();
  });

  it("names the platforms still to come once the catalog has answered", async () => {
    mocks.platforms.mockReturnValue([platform]);
    await reachCredentialStep();

    expect(
      screen.getByText(
        /Coming soon: GitHub Actions, Kubernetes, SPIFFE, Google Cloud\./,
      ),
    ).toBeTruthy();
  });

  it("names no platform as coming soon while the catalog loads", async () => {
    mocks.platformsStatus = "pending";
    await reachCredentialStep();

    expect(screen.getByText("Loading platforms…")).toBeTruthy();
    expect(screen.queryByText(/Coming soon/)).toBeNull();
  });

  it("still offers the custom issuer when the presets fail to load", async () => {
    mocks.platformsStatus = "error";
    await reachCredentialStep();

    expect(screen.getByRole("alert").textContent).toContain(
      "A custom OIDC issuer, under Advanced, still works.",
    );
    expect(screen.queryByText(/Coming soon/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^Advanced:/ }));
    fireEvent.click(screen.getByRole("radio", { name: /Custom OIDC issuer/ }));
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(false);
  });

  it("keeps the advanced options open while the custom issuer is chosen", async () => {
    await reachCredentialStep();
    const advanced = screen.getByRole("button", { name: /^Advanced:/ });
    expect(advanced.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(advanced);
    fireEvent.click(screen.getByRole("radio", { name: /Custom OIDC issuer/ }));
    // Collapsing now would hide the option that is chosen.
    expect(advanced.hasAttribute("disabled")).toBe(true);
    expect(advanced.getAttribute("aria-expanded")).toBe("true");

    // Choosing another option leaves the section as it was, and free to
    // fold away again.
    fireEvent.click(screen.getByRole("radio", { name: /API key/ }));
    expect(advanced.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(advanced);
    expect(advanced.getAttribute("aria-expanded")).toBe("false");
  });

  it("will not set up a linked preset without permission to trust a platform", async () => {
    mocks.canFederate = false;
    mocks.platforms.mockReturnValue([platform]);
    setup(platform.key);
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await continueFromAuthentication();
    await screen.findByRole("radio", { name: /Claude Tag/ });
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(screen.getByText(/Choose an API key instead/)).toBeTruthy();
  });

  it.each([false, true])(
    "creates the agent with issuer assertion reuse %s",
    async (allowAssertionReuse) => {
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
      expect(
        (
          screen.getByRole("checkbox", {
            name: "Allow assertion reuse",
          }) as HTMLInputElement
        ).checked,
      ).toBe(false);
      if (allowAssertionReuse) {
        fireEvent.click(
          screen.getByRole("checkbox", { name: "Allow assertion reuse" }),
        );
      }
      fireEvent.click(
        screen.getByRole("button", { name: "Connect Claude Tag" }),
      );

      await waitFor(() => expect(mocks.admitSubject).toHaveBeenCalledTimes(1));
      // Nothing is created until the setup's own create step runs, and then the
      // agent comes first: the access rule names it.
      expect(mocks.createAgent).toHaveBeenCalledTimes(1);
      expect(mocks.createKey).not.toHaveBeenCalled();
      expect(
        mocks.admitSubject.mock.calls[0]?.[0]?.request
          ?.admitWorkloadSubjectForm,
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
      expect(
        mocks.registerIssuer.mock.calls[0]?.[0]?.request
          ?.registerWorkloadIssuerForm?.allowAssertionReuse,
      ).toBe(allowAssertionReuse);
    },
  );
});

const gkeCluster =
  "https://container.googleapis.com/v1/projects/{project}/locations/{location}/clusters/{cluster}";

const gke = {
  key: "gke",
  displayName: "GKE",
  description: "Workloads on one cluster.",
  icon: "",
  enabled: true,
  issuer: { value: gkeCluster, visibility: "read-only" },
  jwksUri: { value: `${gkeCluster}/jwks`, visibility: "hidden" },
  variables: [
    ["project", "platform", "Project ID", "[a-z][a-z0-9-]{4,28}[a-z0-9]"],
    ["location", "platform", "Location", "[a-z0-9-]+"],
    ["cluster", "platform", "Cluster name", "[a-z0-9-]+"],
    ["namespace", "rule", "Namespace", "[a-z0-9-]+"],
    ["service_account", "rule", "Service account", "[a-z0-9.-]+"],
  ].map(([key, tier, label, pattern]) => ({
    key,
    tier,
    label,
    help: "",
    placeholder: "",
    pattern,
    patternMessage: "",
  })),
  subject: {
    template: "system:serviceaccount:{namespace}:{service_account}",
    wildcard: false,
  },
};

describe("Connecting a GKE cluster", () => {
  it("trusts the cluster on its own and admits one service account", async () => {
    mocks.platforms.mockReturnValue([gke]);
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Billing Worker" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(await screen.findByRole("radio", { name: /GKE/ }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your cluster" });

    for (const [label, value] of [
      ["Project ID", "acme-prod"],
      ["Location", "us-central1"],
      ["Cluster name", "payments"],
    ] as const) {
      fireEvent.change(screen.getByLabelText(label), { target: { value } });
    }
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your workload" });
    for (const [label, value] of [
      ["Namespace", "billing"],
      ["Service account", "invoice-worker"],
    ] as const) {
      fireEvent.change(screen.getByLabelText(label), { target: { value } });
    }
    fireEvent.click(screen.getByRole("button", { name: "Connect GKE" }));

    await waitFor(() => expect(mocks.admitSubject).toHaveBeenCalledTimes(1));
    const issuer =
      "https://container.googleapis.com/v1/projects/acme-prod/locations/us-central1/clusters/payments";
    // Each cluster signs its own tokens, so it is its own trusted platform,
    // named so a second cluster does not collide with it.
    expect(
      mocks.registerIssuer.mock.calls[0]?.[0]?.request
        ?.registerWorkloadIssuerForm,
    ).toMatchObject({
      name: "GKE payments (acme-prod, us-central1)",
      issuer,
      jwksUri: `${issuer}/jwks`,
    });
    expect(
      mocks.admitSubject.mock.calls[0]?.[0]?.request?.admitWorkloadSubjectForm,
    ).toEqual({
      issuer,
      subject: "system:serviceaccount:billing:invoice-worker",
      matchKind: "exact",
      agentId: "agent_new",
      tags: [],
    });
  });

  const sharedServer = {
    userSessionIssuerId: "usi_shared",
    userSessionIssuerSlug: "shared",
    projectId: "",
    projectName: "",
    issuer: "https://app.example.test/oauth/usi/usi_shared",
    tokenEndpoint: "https://app.example.test/oauth/usi/usi_shared/token",
    mcpHost: "app.example.test",
  };

  const githubServer = {
    id: "server_one",
    name: "GitHub",
    slug: "github",
    platformEndpointSlug: "github-mcp",
    projectId: "project_one",
    toolsetId: "toolset_one",
    visibility: "private",
    networkAccessMode: "dual",
    createdAt: new Date(),
    updatedAt: new Date(),
  };

  /** Connects a cluster for an agent that reaches these servers, then shows the pod step. */
  async function reachPodStep(
    servers: (typeof githubServer)[] = [githubServer],
  ) {
    mocks.platforms.mockReturnValue([gke]);
    mocks.mcpServers.mockResolvedValue({ mcpServers: servers });
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Review Agent" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    for (const server of servers) {
      fireEvent.click(
        await screen.findByRole("checkbox", { name: server.name }),
      );
    }
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(await screen.findByRole("radio", { name: /GKE/ }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your cluster" });
    for (const [label, value] of [
      ["Project ID", "example-project"],
      ["Location", "us-west1"],
      ["Cluster name", "example-cluster"],
    ] as const) {
      fireEvent.change(screen.getByLabelText(label), { target: { value } });
    }
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your workload" });
    fireEvent.change(screen.getByLabelText("Namespace"), {
      target: { value: "pr-review" },
    });
    fireEvent.change(screen.getByLabelText("Service account"), {
      target: { value: "review-agent" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect GKE" }));
    await screen.findByRole("heading", {
      name: "Give the pod a Speakeasy token",
    });
  }

  it("names the audience, token endpoint and server URL the pod needs", async () => {
    mocks.tokenEndpoints.mockReturnValue([sharedServer]);
    await reachPodStep();

    expect(screen.getByText("Audience")).toBeTruthy();
    expect(screen.getAllByText(sharedServer.issuer).length).toBeGreaterThan(0);
    expect(document.body.textContent).toContain(
      `audience: ${sharedServer.issuer}`,
    );
    expect(document.body.textContent).not.toContain("SPEAKEASY_AUDIENCE");

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", {
      name: "Exchange the token and call the server",
    });
    const serverUrl = new URL("/mcp/github-mcp", window.location.origin).href;
    expect(screen.getByText("Call GitHub with the access token")).toBeTruthy();
    expect(
      screen.getAllByText(sharedServer.tokenEndpoint).length,
    ).toBeGreaterThan(0);
    expect(screen.getAllByText(serverUrl).length).toBeGreaterThan(0);
    const text = document.body.textContent ?? "";
    // Quoted, so nothing in a value is read as shell syntax.
    expect(text).toContain(`TOKEN_ENDPOINT='${sharedServer.tokenEndpoint}'`);
    expect(text).toContain(`MCP_URL='${serverUrl}'`);
    // No resource: the shared authorization server then issues one token for
    // all of the agent's servers. Naming one would bind the token to it, and
    // the pod's identity token is accepted only once.
    expect(text).not.toContain("resource=");
    expect(text).not.toContain("SPEAKEASY_TOKEN_ENDPOINT");
  });

  it("says why the values are missing and which placeholders stand in", async () => {
    await reachPodStep();

    expect(
      screen.getByText(/usable shared Speakeasy authorization server/),
    ).toBeTruthy();
    expect(document.body.textContent).toContain("audience: SPEAKEASY_AUDIENCE");
    expect(
      screen.getByText(/Until then, the snippet below shows/),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", {
      name: "Exchange the token and call the server",
    });
    expect(
      screen.getByText(/usable shared Speakeasy authorization server/),
    ).toBeTruthy();
    expect(document.body.textContent).toContain(
      "TOKEN_ENDPOINT='SPEAKEASY_TOKEN_ENDPOINT'",
    );
  });

  async function reachExchangeStep(servers: (typeof githubServer)[]) {
    mocks.tokenEndpoints.mockReturnValue([sharedServer]);
    await reachPodStep(servers);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", {
      name: "Exchange the token and call the server",
    });
  }

  it("targets the server picked from the agent's servers, at its own public URL", async () => {
    // Radix Select reads pointer capture and scrolls options into view, which
    // jsdom does not implement.
    for (const [name, value] of [
      ["hasPointerCapture", () => false],
      ["releasePointerCapture", () => undefined],
      ["scrollIntoView", () => undefined],
    ] as const) {
      if (!(name in Element.prototype)) {
        Object.defineProperty(Element.prototype, name, {
          value,
          configurable: true,
        });
      }
    }
    const linearUrl = "https://mcp.example.test/mcp/linear";
    await reachExchangeStep([
      githubServer,
      {
        ...githubServer,
        id: "server_two",
        name: "Linear",
        slug: "linear",
        toolsetId: "toolset_two",
        platformEndpointSlug: "linear-platform",
        primaryEndpointUrl: linearUrl,
      } as typeof githubServer,
    ]);

    const githubUrl = new URL("/mcp/github-mcp", window.location.origin).href;
    expect(document.body.textContent).toContain(`MCP_URL='${githubUrl}'`);

    const picker = screen.getByRole("combobox", { name: "MCP server" });
    fireEvent.keyDown(picker, { key: "Enter" });
    fireEvent.click(await screen.findByRole("option", { name: "Linear" }));

    await screen.findByText("Call Linear with the access token");
    expect(screen.getAllByText(linearUrl).length).toBeGreaterThan(0);
    // Its own public URL wins over its platform route.
    expect(document.body.textContent).toContain(`MCP_URL='${linearUrl}'`);
    expect(document.body.textContent).not.toContain("linear-platform");
  });

  it("withholds a server URL that would carry the access token over plaintext", async () => {
    await reachExchangeStep([
      {
        ...githubServer,
        primaryEndpointUrl: "http://mcp.example.test/mcp/github",
      } as typeof githubServer,
    ]);

    expect(screen.getByText(/only served over plaintext HTTP/)).toBeTruthy();
    expect(document.body.textContent).not.toContain("http://mcp.example.test");
    expect(document.body.textContent).toContain("MCP_URL='MCP_SERVER_URL'");
  });

  it("calls a server served at the root of its domain, without naming it as a resource", async () => {
    const domainRoot = "https://mcp.example.test";
    await reachExchangeStep([
      {
        ...githubServer,
        primaryEndpointUrl: domainRoot,
      } as typeof githubServer,
    ]);

    expect(screen.getAllByText(domainRoot).length).toBeGreaterThan(0);
    const text = document.body.textContent ?? "";
    expect(text).toContain(`MCP_URL='${domainRoot}'`);
    expect(text).not.toContain("resource=");
  });
});

describe("Agent project", () => {
  it("binds a new agent to the project it is created in", async () => {
    // Not a choice any more: an agent belongs where it is made. It was once
    // a radio on this step, which asked the same question the server list
    // answers and gave the two somewhere to disagree.
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Project bound" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );

    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    const form = mocks.createAgent.mock.calls[0]![0].createAgentForm;
    expect(form.projectId).toBe("project_one");
  });

  it("creates nothing until a project resolves", async () => {
    // useProject yields an empty id first. Creating on it would bind the
    // agent to no project — and under All servers write a wildcard with no
    // project on it, which reaches the whole organization.
    mocks.projectId = "";
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "No project yet" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    const create = (await screen.findByRole("button", {
      name: "Create agent",
    })) as HTMLButtonElement;
    expect(create.disabled).toBe(true);
    fireEvent.click(create);

    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it("reports All servers rather than a count of the rows", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Everything here" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await enabledAllServers());

    // The individual rows are not the choice, so counting them would read as
    // "0 of 1 servers selected" beside a toggle that reaches every one.
    expect(screen.getByText("All servers selected.")).toBeTruthy();
  });

  it("does not offer All servers to someone who cannot reach every server", async () => {
    // A wildcard is refused when its creator is kept off any server it covers,
    // including one they never see in this list. Offering it anyway ended in
    // "requested agent access exceeds your permissions" on Create agent.
    mocks.checkPolicyGrants.mockImplementation(answerChecks({ "*": "caller" }));
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Everything here" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const all = await screen.findByRole("checkbox", { name: "All servers" });
    expect(
      await screen.findByText(
        /You cannot reach every server in Project one, so this agent cannot either/,
      ),
    ).toBeTruthy();
    expect(all.hasAttribute("disabled")).toBe(true);
    fireEvent.click(all);
    expect(all.getAttribute("data-state")).toBe("unchecked");
    expect(screen.getByText("Select at least one MCP server.")).toBeTruthy();
    // Asked about the grants Create agent could write, for an agent that does
    // not exist yet: every server in its project, and each server on its own.
    expect(mocks.checkPolicyGrants.mock.calls[0]?.[0]).toEqual({
      checkAgentPolicyGrantsForm: {
        policyGrants: [
          {
            effect: "allow",
            scope: "mcp:connect",
            selector: {
              resourceKind: "mcp",
              resourceId: "*",
              projectId: "project_one",
            },
          },
          {
            effect: "allow",
            scope: "mcp:connect",
            selector: {
              resourceKind: "mcp",
              resourceId: "toolset_one",
              projectId: "project_one",
            },
          },
        ],
      },
    });

    // The servers it can reach are still a choice.
    fireEvent.click(screen.getByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants?.[0]
        ?.selector?.resourceId,
    ).toBe("toolset_one");
  });

  it("does not offer a server its creator cannot reach", async () => {
    // The same refusal, one server at a time: a server the creator can see
    // but not connect to is listed, and a grant on it is refused just the
    // same. "Select shown" must not walk into it either.
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
        {
          id: "server_two",
          name: "Linear",
          slug: "linear",
          projectId: "project_one",
          toolsetId: "toolset_two",
          visibility: "private",
          networkAccessMode: "dual",
          createdAt: new Date(),
          updatedAt: new Date(),
        },
      ],
    });
    mocks.checkPolicyGrants.mockImplementation(
      answerChecks({ "*": "caller", toolset_two: "caller" }),
    );
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Some servers" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(
      await screen.findByText(
        "You cannot reach this server, so this agent cannot either.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("checkbox", { name: "Linear" }).hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Select shown" }));
    expect(screen.getByText("1 of 2 servers selected.")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants?.map(
        (form: { selector: { resourceId: string } }) =>
          form.selector.resourceId,
      ),
    ).toEqual(["toolset_one"]);
  });

  it("saves no server the check has not answered for", async () => {
    // Ticked before the answer arrived, or after the check failed: either way
    // the server has not said the grant fits, so nothing is written yet.
    mocks.checkPolicyGrants.mockRejectedValueOnce(new Error("network down"));
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Unchecked" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      await screen.findByText("Could not check which servers you can reach."),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("checkbox", { name: "GitHub" }));
    const next = screen.getByRole("button", {
      name: "Continue",
    }) as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    expect(
      screen.getByText(
        "Could not check that you can reach the servers you chose. Check again above the list.",
      ),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    await waitFor(() => expect(next.disabled).toBe(false));
  });

  it.each(["All servers", "GitHub"])(
    "blocks %s after a failed recheck until a retry succeeds",
    async (selection) => {
      setup();
      fireEvent.change(screen.getByLabelText("Name"), {
        target: { value: "Rechecked access" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await enabledAllServers();
      fireEvent.click(screen.getByRole("checkbox", { name: selection }));
      const next = screen.getByRole("button", {
        name: "Continue",
      }) as HTMLButtonElement;
      expect(next.disabled).toBe(false);

      mocks.checkPolicyGrants.mockRejectedValueOnce(new Error("network down"));
      fireEvent(document, new Event("visibilitychange"));
      await screen.findByText("Could not check which servers you can reach.");
      expect(next.disabled).toBe(true);
      fireEvent.click(next);
      expect(mocks.createAgent).not.toHaveBeenCalled();

      let resolveRetry!: (answer: { results: { allowed: boolean }[] }) => void;
      mocks.checkPolicyGrants.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveRetry = resolve;
          }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Check again" }));
      await waitFor(() =>
        expect(mocks.checkPolicyGrants).toHaveBeenCalledTimes(3),
      );
      expect(next.disabled).toBe(true);
      resolveRetry({ results: [{ allowed: true }, { allowed: true }] });
      await waitFor(() => expect(next.disabled).toBe(false));

      fireEvent.click(next);
      await continueFromAuthentication();
      fireEvent.click(
        await screen.findByRole("button", { name: "Create agent" }),
      );
      await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
      expect(
        mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants?.[0]
          ?.selector?.resourceId,
      ).toBe(selection === "All servers" ? "*" : "toolset_one");
    },
  );

  it("waits for pending permission checks before continuing or offering All servers", async () => {
    let resolveCheck!: (answer: { results: { allowed: boolean }[] }) => void;
    mocks.checkPolicyGrants.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveCheck = resolve;
        }),
    );
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Waiting for access" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const all = await screen.findByRole("checkbox", { name: "All servers" });
    expect(all.hasAttribute("disabled")).toBe(true);
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    const next = screen.getByRole("button", {
      name: "Continue",
    }) as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    fireEvent.click(next);
    expect(mocks.createAgent).not.toHaveBeenCalled();

    await waitFor(() =>
      expect(mocks.checkPolicyGrants).toHaveBeenCalledTimes(1),
    );
    resolveCheck({ results: [{ allowed: true }, { allowed: true }] });
    await waitFor(() => expect(next.disabled).toBe(false));
    expect(all.hasAttribute("disabled")).toBe(false);
    fireEvent.click(next);
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
  });

  it("keeps selections unsavable when a later permission batch fails and maps retried answers correctly", async () => {
    mocks.mcpServers.mockResolvedValue({
      mcpServers: Array.from({ length: 101 }, (_, index) => ({
        id: `server_${index}`,
        name: `Server ${index}`,
        slug: `server-${index}`,
        projectId: "project_one",
        toolsetId: `toolset_${index}`,
        visibility: "private",
        networkAccessMode: "dual",
        createdAt: new Date(),
        updatedAt: new Date(),
      })),
    });
    const answer = answerChecks({ "*": "caller", toolset_100: "caller" });
    mocks.checkPolicyGrants
      .mockImplementation(answer)
      .mockImplementationOnce(answer)
      .mockRejectedValueOnce(new Error("second batch unavailable"));
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Several batches" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("Could not check which servers you can reach.");

    fireEvent.click(screen.getByRole("checkbox", { name: "Server 100" }));
    const next = screen.getByRole("button", {
      name: "Continue",
    }) as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    expect(mocks.createAgent).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    await screen.findByText(/Deselect Server 100:/);
    expect(next.disabled).toBe(true);
    expect(
      mocks.checkPolicyGrants.mock.calls.map(
        (call) =>
          (call[0] as CheckRequest).checkAgentPolicyGrantsForm.policyGrants
            .length,
      ),
    ).toEqual([100, 2, 100, 2]);

    fireEvent.click(screen.getByRole("checkbox", { name: "Server 100" }));
    expect(
      screen
        .getByRole("checkbox", { name: "Server 100" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: "Server 0" }));
    expect(next.disabled).toBe(false);
    fireEvent.click(next);
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );
    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants?.map(
        (form: { selector: { resourceId: string } }) =>
          form.selector.resourceId,
      ),
    ).toEqual(["toolset_0"]);
  });

  it("holds All servers until the check answers, and retries one that failed", async () => {
    mocks.checkPolicyGrants.mockRejectedValueOnce(new Error("network down"));
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Everything here" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const all = await screen.findByRole("checkbox", { name: "All servers" });
    expect(
      await screen.findByText(
        /Could not check that you can reach every server in Project one/,
      ),
    ).toBeTruthy();
    expect(all.hasAttribute("disabled")).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    fireEvent.click(await enabledAllServers());
    expect(screen.getByText("All servers selected.")).toBeTruthy();
  });

  it("gives All servers the project, so it does not reach past the agent", async () => {
    // A wildcard with no project on it reaches every project. The agent
    // belongs to one, so the grant has to say which.
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Everything here" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await enabledAllServers());
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );

    await waitFor(() => expect(mocks.createAgent).toHaveBeenCalledTimes(1));
    const form = mocks.createAgent.mock.calls[0]![0].createAgentForm;
    expect(form.policyGrants).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "*",
          projectId: "project_one",
        },
      },
    ]);
  });
});

describe("Editing an agent that already exists", () => {
  const agent = {
    id: "agent_one",
    name: "Release Bot",
    lifecycle: "active",
    // Bound to a project, like every agent made since the binding stopped
    // being optional. The grants written for it have to name it.
    projectId: "project_one",
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
          <CredentialsWizard
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

  async function authenticationSave() {
    if (screen.queryByRole("heading", { name: "MCP Selection" })) {
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    }
    return screen.findByRole("button", { name: "Save changes and exit" });
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
    expect(credential.getAttribute("aria-label")).toBe("Credential (done)");
    expect(credential.textContent).not.toContain("Current");
    // The step the reader is on keeps its place even though everything from
    // the credential on is settled.
    const current = screen.getByRole("button", { name: /Name/ });
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
    fireEvent.click(await authenticationSave());

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
    fireEvent.click(await authenticationSave());

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
    fireEvent.click(await authenticationSave());

    await waitFor(() => expect(mocks.deleteGrant).toHaveBeenCalledTimes(1));
    expect(
      mocks.deleteGrant.mock.calls[0]?.[0]?.request?.deleteAgentPolicyGrantForm,
    ).toEqual({
      agentId: "agent_one",
      grantId: "grant_one",
      expectedPolicy: {
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "toolset_one",
          projectId: "project_one",
        },
      },
    });
    expect(mocks.createGrant).not.toHaveBeenCalled();
  });

  it("grants a server that was not reachable before", async () => {
    editing({ grants: [] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const server = await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(server);
    fireEvent.click(await authenticationSave());

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

  it("keeps an unavailable assignment removable when its issuer has no clients", async () => {
    mocks.mcpServers.mockResolvedValue({
      mcpServers: [
        {
          id: "server_one",
          name: "GitHub",
          slug: "github",
          projectId: "project_one",
          toolsetId: "toolset_one",
          userSessionIssuerId: "issuer_one",
          visibility: "private",
          networkAccessMode: "dual",
          createdAt: new Date(),
          updatedAt: new Date(),
        },
      ],
    });
    mocks.bindings.mockResolvedValue([
      {
        id: "binding_unavailable",
        principalId: "agent_one",
        remoteSessionClientId: "deleted_client",
        remoteSessionId: "deleted_session",
        userSessionIssuerId: "issuer_one",
      },
    ]);
    mocks.sessionClients.mockResolvedValue([]);
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("checkbox", { name: "GitHub" });
    await authenticationSave();
    // The first row starts expanded.
    await screen.findByRole("button", {
      name: /GitHub.*Requires connection/,
    });
    expect(
      await screen.findByRole("button", {
        name: "Disconnect unavailable account",
      }),
    ).toBeTruthy();
    expect(
      screen.queryByText(
        "This server uses a shared credential. No personal connection is needed.",
      ),
    ).toBeNull();
  });

  it("creates no agent: this one already exists", async () => {
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("checkbox", { name: "GitHub" });
    fireEvent.click(await authenticationSave());

    await waitFor(() => expect(mocks.renameAgent).not.toHaveBeenCalled());
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  const WILDCARD = {
    id: "grant_all",
    effect: "allow",
    scope: "mcp:connect",
    selector: { resourceKind: "mcp", resourceId: "*" },
    createdAt: new Date(),
    updatedAt: new Date(),
  };

  it("opens on All servers when a wildcard grant already reaches every one", async () => {
    // The picker says what the policy allows rather than coming up empty
    // beside a grant that reaches all of them.
    editing({ grants: [WILDCARD] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const all = (await screen.findByRole("checkbox", {
      name: "All servers",
    })) as HTMLInputElement;
    await waitFor(() => expect(all.getAttribute("data-state")).toBe("checked"));
    // Every row reads as reached, because it is, and none of them is the
    // thing to change while the wildcard stands.
    const github = screen.getByRole("checkbox", {
      name: "GitHub",
    }) as HTMLInputElement;
    expect(github.getAttribute("data-state")).toBe("checked");
    expect(github.hasAttribute("disabled")).toBe(true);
  });

  it("keeps the wildcard grant when All servers is left on", async () => {
    const { onDone } = editing({ grants: [WILDCARD] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("checkbox", { name: "All servers" });
    fireEvent.click(await authenticationSave());

    await waitFor(() => expect(onDone).toHaveBeenCalled());
    // Nothing to write: the stored reach and the chosen one are the same.
    expect(mocks.deleteGrant).not.toHaveBeenCalled();
    expect(mocks.createGrant).not.toHaveBeenCalled();
  });

  it("replaces the wildcard grant with the list when All servers is turned off", async () => {
    editing({ grants: [WILDCARD] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await enabledAllServers());
    fireEvent.click(screen.getByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(await authenticationSave());

    await waitFor(() => expect(mocks.deleteGrant).toHaveBeenCalledTimes(1));
    expect(
      mocks.deleteGrant.mock.calls[0]?.[0]?.request?.deleteAgentPolicyGrantForm,
    ).toEqual({
      agentId: "agent_one",
      grantId: "grant_all",
      expectedPolicy: { scope: WILDCARD.scope, selector: WILDCARD.selector },
    });
    // The chosen server is granted before the wildcard goes, so a failure
    // between the two leaves too much reach rather than too little.
    expect(mocks.createGrant).toHaveBeenCalledTimes(1);
    expect(mocks.createGrant.mock.invocationCallOrder[0] ?? 0).toBeLessThan(
      mocks.deleteGrant.mock.invocationCallOrder[0] ?? 0,
    );
  });

  it("writes a wildcard grant when All servers is turned on", async () => {
    // One grant, not one per server: the list it would otherwise name is
    // today's inventory, and the promise covers servers added later.
    editing({ grants: [] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await enabledAllServers());
    fireEvent.click(await authenticationSave());

    await waitFor(() => expect(mocks.createGrant).toHaveBeenCalledTimes(1));
    // Carries the agent's project: a wildcard with no project on it reaches
    // every project, which is wider than the agent it belongs to.
    expect(
      mocks.createGrant.mock.calls[0]?.[0]?.request?.createAgentPolicyGrantForm
        ?.selector,
    ).toEqual({
      resourceKind: "mcp",
      resourceId: "*",
      projectId: "project_one",
    });
  });

  it("says so when the agent's owner cannot reach every server", async () => {
    mocks.checkPolicyGrants.mockImplementation(answerChecks({ "*": "owner" }));
    editing({ grants: [] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const all = await screen.findByRole("checkbox", { name: "All servers" });
    expect(
      await screen.findByText(
        /This agent's owner cannot reach every server in Project one/,
      ),
    ).toBeTruthy();
    expect(all.hasAttribute("disabled")).toBe(true);
    // Asked about this agent, so the server weighs its owner's access too.
    expect(
      mocks.checkPolicyGrants.mock.calls[0]?.[0]?.checkAgentPolicyGrantsForm
        ?.agentId,
    ).toBe("agent_one");
  });

  it("does not ask about a wildcard the agent already holds", async () => {
    // Turning All servers off and on again writes nothing, so there is
    // nothing for the check to refuse.
    mocks.checkPolicyGrants.mockImplementation(
      answerChecks({ "*": "caller", toolset_one: "caller" }),
    );
    editing({ grants: [WILDCARD] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const all = await screen.findByRole("checkbox", { name: "All servers" });
    await waitFor(() => expect(all.getAttribute("data-state")).toBe("checked"));
    fireEvent.click(all);
    fireEvent.click(all);
    expect(all.getAttribute("data-state")).toBe("checked");
    expect(
      mocks.checkPolicyGrants.mock.calls.flatMap((call) =>
        (call[0] as CheckRequest).checkAgentPolicyGrantsForm.policyGrants.map(
          (grant) => grant.selector.resourceId,
        ),
      ),
    ).not.toContain("*");
  });

  const WILDCARD_TWO = { ...WILDCARD, id: "grant_all_two" };

  it("keeps an organization-wide wildcard's reach when a key is issued", async () => {
    // The agent is labelled with one project but its wildcard carries none,
    // so it reaches every project. Issuing a key must not quietly narrow it.
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
        {
          id: "server_two",
          name: "Linear",
          slug: "linear",
          projectId: "project_two",
          toolsetId: "toolset_two",
          visibility: "private",
          networkAccessMode: "dual",
          createdAt: new Date(),
          updatedAt: new Date(),
        },
      ],
    });
    // Both servers delegable, or issuance stops on the one discovery did not
    // cover and the key is never asked for.
    mocks.listDelegableGrants.mockResolvedValue([
      grant,
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "toolset_two",
          projectId: "project_two",
        },
      },
    ]);
    editing({ grants: [WILDCARD] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const all = await screen.findByRole("checkbox", { name: "All servers" });
    await waitFor(() => expect(all.getAttribute("data-state")).toBe("checked"));
    // "All servers" names the reach it actually has, not the agent's project.
    expect(screen.getByText(/Every server in Example Org/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Credential/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Issue key" }));

    await waitFor(() =>
      expect(mocks.listDelegableGrants).toHaveBeenCalledTimes(1),
    );
    expect(mocks.listDelegableGrants.mock.calls[0]?.[0]?.toolsetIds).toEqual([
      "toolset_one",
      "toolset_two",
    ]);
    // And the key is issued for both, not just the agent's own project's.
    await waitFor(() => expect(mocks.createKey).toHaveBeenCalledTimes(1));
    expect(
      mocks.createKey.mock.calls[0]?.[0]?.createKeyForm?.requestedGrants?.map(
        (form: { selector: { resourceId: string } }) =>
          form.selector.resourceId,
      ),
    ).toEqual(["toolset_one", "toolset_two"]);
  });

  it("creates the wildcard once when a later removal fails", async () => {
    // The wildcard goes in first, so a failed removal after it must not make
    // the retry write a second one.
    mocks.deleteGrant.mockRejectedValueOnce(new Error("Grant is locked"));
    mocks.createGrant.mockResolvedValue({ id: "grant_all_new" });
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await enabledAllServers());
    const save = await authenticationSave();
    fireEvent.click(save);
    expect(await screen.findByText("Grant is locked")).toBeTruthy();

    fireEvent.click(await authenticationSave());
    await waitFor(() => expect(mocks.deleteGrant).toHaveBeenCalledTimes(2));
    expect(mocks.createGrant).toHaveBeenCalledTimes(1);
  });

  it("does not re-delete a wildcard a failed save already removed", async () => {
    // Two wildcard rows, the second delete failing: the retry asks for the
    // one still standing rather than for an id that is already gone.
    mocks.deleteGrant
      .mockResolvedValueOnce({})
      .mockRejectedValueOnce(new Error("Grant is locked"));
    editing({ grants: [WILDCARD, WILDCARD_TWO] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await enabledAllServers());
    fireEvent.click(screen.getByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(await authenticationSave());
    expect(await screen.findByText("Grant is locked")).toBeTruthy();

    fireEvent.click(await authenticationSave());
    await waitFor(() => expect(mocks.deleteGrant).toHaveBeenCalledTimes(3));
    expect(
      mocks.deleteGrant.mock.calls.map(
        (call) => call[0]?.request?.deleteAgentPolicyGrantForm?.grantId,
      ),
    ).toEqual(["grant_all", "grant_all_two", "grant_all_two"]);
  });

  it("keeps a conflicting deletion pending with its original policy on retry", async () => {
    mocks.deleteGrant.mockRejectedValueOnce(
      Object.assign(new Error("Policy changed; reopen to review"), {
        statusCode: 409,
      }),
    );
    const { onDone } = editing({ grants: [WILDCARD, WILDCARD_TWO] });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "All servers" }),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(await authenticationSave());
    await screen.findByText("Policy changed; reopen to review");
    expect(mocks.deleteGrant).toHaveBeenCalledTimes(1);
    expect(onDone).not.toHaveBeenCalled();
    expect(
      mocks.deleteGrant.mock.calls[0]?.[0]?.request?.deleteAgentPolicyGrantForm,
    ).toEqual({
      agentId: "agent_one",
      grantId: WILDCARD.id,
      expectedPolicy: { scope: WILDCARD.scope, selector: WILDCARD.selector },
    });
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Save changes and exit",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    fireEvent.click(await authenticationSave());
    await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
    expect(mocks.createGrant).toHaveBeenCalledTimes(1);
    expect(
      mocks.deleteGrant.mock.calls.map(
        (call) => call[0]?.request?.deleteAgentPolicyGrantForm,
      ),
    ).toEqual([
      {
        agentId: "agent_one",
        grantId: WILDCARD.id,
        expectedPolicy: { scope: WILDCARD.scope, selector: WILDCARD.selector },
      },
      {
        agentId: "agent_one",
        grantId: WILDCARD.id,
        expectedPolicy: { scope: WILDCARD.scope, selector: WILDCARD.selector },
      },
      {
        agentId: "agent_one",
        grantId: WILDCARD_TWO.id,
        expectedPolicy: {
          scope: WILDCARD_TWO.scope,
          selector: WILDCARD_TWO.selector,
        },
      },
    ]);
  });

  it("will not save while the server list has not loaded", async () => {
    // An unread list leaves the selection empty, which is indistinguishable
    // from "no servers chosen" — and saving that withdraws every grant.
    mocks.mcpServers.mockRejectedValue(new Error("server list refused"));
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const save = (await authenticationSave()) as HTMLButtonElement;
    await waitFor(() => expect(save.disabled).toBe(true));
    fireEvent.click(save);
    expect(mocks.deleteGrant).not.toHaveBeenCalled();
    expect(mocks.createGrant).not.toHaveBeenCalled();
  });

  it("drops the agent's cached policy once the grants are written", async () => {
    editing();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(await authenticationSave());

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
    fireEvent.click(await authenticationSave());

    expect(await screen.findByText("Grant is locked")).toBeTruthy();
    expect(onDone).not.toHaveBeenCalled();
  });
  describe("its GKE workload", () => {
    const cluster =
      "https://container.googleapis.com/v1/projects/acme-prod/locations/us-central1/clusters/payments";
    const admission = {
      id: "admission_one",
      agentId: "agent_one",
      agentName: "Release Bot",
      issuer: cluster,
      issuerName: "GKE payments (acme-prod, us-central1)",
      matchKind: "exact",
      name: "",
      organizationId: "org_example",
      projectId: "",
      subject: "system:serviceaccount:default:my-workload",
      tags: [],
      wildcardActive: false,
      workloadIssuerId: "issuer_one",
      createdAt: new Date(),
      updatedAt: new Date(),
    };

    beforeEach(() => {
      mocks.platforms.mockReturnValue([gke]);
      mocks.workloadIssuers.mockReturnValue([
        {
          id: "issuer_one",
          name: admission.issuerName,
          issuer: cluster,
          jwksUri: `${cluster}/jwks`,
          organizationId: "org_example",
          projectId: "",
        },
      ]);
      mocks.workloadAdmissions.mockReturnValue([admission]);
    });

    async function reachWorkloadStep() {
      editing();
      fireEvent.click(screen.getByRole("button", { name: /Credential/ }));
      fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Your cluster" });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Your workload" });
    }

    it("reads an unchanged workload back without writing", async () => {
      await reachWorkloadStep();
      expect(
        (screen.getByLabelText("Namespace") as HTMLInputElement).value,
      ).toBe("default");
      expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      expect(mocks.admitSubject).not.toHaveBeenCalled();
      expect(mocks.withdrawSubject).not.toHaveBeenCalled();
    });

    it("saves a changed service account by replacing the rule", async () => {
      await reachWorkloadStep();
      fireEvent.change(screen.getByLabelText("Namespace"), {
        target: { value: "pr-review" },
      });
      fireEvent.change(screen.getByLabelText("Service account"), {
        target: { value: "review-agent" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(mocks.withdrawSubject).toHaveBeenCalledOnce());
      // The cluster is already trusted, so only the rule changes.
      expect(mocks.registerIssuer).not.toHaveBeenCalled();
      expect(mocks.admitSubject.mock.calls[0]?.[0]?.request).toEqual({
        admitWorkloadSubjectForm: {
          projectScoped: false,
          issuer: cluster,
          subject: "system:serviceaccount:pr-review:review-agent",
          matchKind: "exact",
          agentId: "agent_one",
          tags: [],
        },
      });
      // The new rule first, so the agent is never left without one.
      expect(mocks.admitSubject.mock.invocationCallOrder[0]!).toBeLessThan(
        mocks.withdrawSubject.mock.invocationCallOrder[0]!,
      );
      expect(mocks.withdrawSubject.mock.calls[0]?.[0]).toEqual({
        request: { id: "admission_one" },
      });
    });

    it("keeps a project's rule with that project", async () => {
      mocks.workloadAdmissions.mockReturnValue([
        { ...admission, projectId: "project_two" },
      ]);
      await reachWorkloadStep();
      fireEvent.change(screen.getByLabelText("Service account"), {
        target: { value: "review-agent" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(mocks.withdrawSubject).toHaveBeenCalledOnce());
      expect(mocks.admitSubject.mock.calls[0]?.[0]?.request).toMatchObject({
        gramProject: "project-two",
        admitWorkloadSubjectForm: { projectScoped: true },
      });
      expect(mocks.withdrawSubject.mock.calls[0]?.[0]).toEqual({
        request: { id: "admission_one" },
      });
    });

    it("trusts a new cluster and moves the rule to it", async () => {
      editing();
      fireEvent.click(screen.getByRole("button", { name: /Credential/ }));
      fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Your cluster" });
      fireEvent.change(screen.getByLabelText("Cluster name"), {
        target: { value: "review" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Your workload" });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(mocks.withdrawSubject).toHaveBeenCalledOnce());
      const moved = cluster.replace(/payments$/, "review");
      expect(
        mocks.registerIssuer.mock.calls[0]?.[0]?.request
          ?.registerWorkloadIssuerForm?.issuer,
      ).toBe(moved);
      expect(
        mocks.admitSubject.mock.calls[0]?.[0]?.request
          ?.admitWorkloadSubjectForm,
      ).toMatchObject({
        issuer: moved,
        subject: admission.subject,
      });
    });

    it("keeps the replaced rule's label and tags", async () => {
      mocks.workloadAdmissions.mockReturnValue([
        { ...admission, name: "Billing pods", tags: ["prod"] },
      ]);
      await reachWorkloadStep();
      fireEvent.change(screen.getByLabelText("Service account"), {
        target: { value: "review-agent" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(mocks.admitSubject).toHaveBeenCalledOnce());
      expect(
        mocks.admitSubject.mock.calls[0]?.[0]?.request
          ?.admitWorkloadSubjectForm,
      ).toMatchObject({ name: "Billing pods", tags: ["prod"] });
    });

    it("withdraws every superseded rule after a failed withdrawal and another change", async () => {
      mocks.admitSubject.mockImplementation(
        async ({
          request,
        }: {
          request: { admitWorkloadSubjectForm: Record<string, string> };
        }) => {
          const form = request.admitWorkloadSubjectForm;
          return {
            issuers: [],
            admissions: [
              {
                ...admission,
                id: `admission_${form["subject"]?.split(":").pop()}`,
                issuer: form["issuer"],
                subject: form["subject"],
                matchKind: form["matchKind"],
              },
            ],
          };
        },
      );
      mocks.withdrawSubject.mockRejectedValueOnce(new Error("boom"));
      await reachWorkloadStep();
      fireEvent.change(screen.getByLabelText("Service account"), {
        target: { value: "first-try" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
      await screen.findByText(/the old one could not be removed/);

      fireEvent.change(screen.getByLabelText("Service account"), {
        target: { value: "review-agent" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() =>
        expect(mocks.withdrawSubject).toHaveBeenCalledTimes(3),
      );
      expect(mocks.admitSubject).toHaveBeenCalledTimes(2);
      // The original rule, retried, and the first replacement the second
      // change superseded: only the latest rule is left.
      expect(
        mocks.withdrawSubject.mock.calls.slice(1).map((call) => call[0]),
      ).toEqual([
        { request: { id: "admission_one" } },
        { request: { id: "admission_first-try" } },
      ]);
    });

    it("lets someone without workload permission read the rule but not change it", async () => {
      mocks.canFederate = false;
      await reachWorkloadStep();
      // Disabled through the fieldset around it. Browsers disable every
      // control inside; jsdom does not reflect that on the input itself.
      expect(
        screen.getByLabelText("Service account").closest("fieldset")?.disabled,
      ).toBe(true);
      expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      expect(mocks.admitSubject).not.toHaveBeenCalled();
      expect(mocks.withdrawSubject).not.toHaveBeenCalled();
    });

    it("retries only the withdrawal when that alone failed", async () => {
      mocks.withdrawSubject.mockRejectedValueOnce(new Error("boom"));
      await reachWorkloadStep();
      fireEvent.change(screen.getByLabelText("Service account"), {
        target: { value: "review-agent" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
      await screen.findByText(/the old one could not be removed/);

      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
      await waitFor(() =>
        expect(mocks.withdrawSubject).toHaveBeenCalledTimes(2),
      );
      expect(mocks.admitSubject).toHaveBeenCalledOnce();
    });
  });
  describe("its Claude Tag or custom issuer", () => {
    const base = {
      agentId: "agent_one",
      agentName: "Release Bot",
      name: "",
      organizationId: "org_example",
      projectId: "",
      tags: [],
      wildcardActive: false,
      createdAt: new Date(),
      updatedAt: new Date(),
    };

    function trust(id: string, issuer: string) {
      mocks.workloadIssuers.mockReturnValue([
        {
          id,
          name: "Trusted platform",
          issuer,
          jwksUri: `${issuer}/jwks`,
          organizationId: "org_example",
          projectId: "",
        },
      ]);
    }

    it("replaces a Claude Tag rule when the organization changes", async () => {
      mocks.platforms.mockReturnValue([platform]);
      trust("issuer_tag", "https://issuer.example");
      mocks.workloadAdmissions.mockReturnValue([
        {
          ...base,
          id: "admission_tag",
          issuer: "https://issuer.example",
          issuerName: "Claude Tag",
          matchKind: "wildcard",
          subject: "spiffe://example/org/org-123/agent/*",
          workloadIssuerId: "issuer_tag",
        },
      ]);
      editing();
      fireEvent.click(screen.getByRole("button", { name: /Credential/ }));
      fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Anthropic organization" });
      const field = screen.getByLabelText(
        "Anthropic organization ID",
      ) as HTMLInputElement;
      expect(field.value).toBe("org-123");
      expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();

      fireEvent.change(field, { target: { value: "org-456" } });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(mocks.withdrawSubject).toHaveBeenCalledOnce());
      expect(
        mocks.admitSubject.mock.calls[0]?.[0]?.request
          ?.admitWorkloadSubjectForm,
      ).toMatchObject({
        issuer: "https://issuer.example",
        subject: "spiffe://example/org/org-456/agent/*",
        matchKind: "wildcard",
        agentId: "agent_one",
      });
      expect(mocks.withdrawSubject.mock.calls[0]?.[0]).toEqual({
        request: { id: "admission_tag" },
      });
    });

    it("keeps a custom rule with its project under an organization issuer", async () => {
      trust("issuer_custom", "https://idp.example");
      mocks.workloadAdmissions.mockReturnValue([
        {
          ...base,
          id: "admission_custom",
          issuer: "https://idp.example",
          issuerName: "Trusted platform",
          matchKind: "exact",
          subject: "workload-a",
          projectId: "project_two",
          workloadIssuerId: "issuer_custom",
        },
      ]);
      editing();
      fireEvent.click(screen.getByRole("button", { name: /Credential/ }));
      fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Identity provider" });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Which workloads" });
      fireEvent.change(screen.getByLabelText("Subject"), {
        target: { value: "workload-b" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(mocks.withdrawSubject).toHaveBeenCalledOnce());
      expect(mocks.admitSubject.mock.calls[0]?.[0]?.request).toMatchObject({
        gramProject: "project-two",
        admitWorkloadSubjectForm: {
          projectScoped: true,
          subject: "workload-b",
        },
      });
    });

    it("fills in a custom issuer's rule and replaces it when changed", async () => {
      trust("issuer_custom", "https://idp.example");
      mocks.workloadAdmissions.mockReturnValue([
        {
          ...base,
          id: "admission_custom",
          issuer: "https://idp.example",
          issuerName: "Trusted platform",
          matchKind: "wildcard",
          subject: "workloads/billing/*",
          workloadIssuerId: "issuer_custom",
        },
      ]);
      editing();
      fireEvent.click(screen.getByRole("button", { name: /Credential/ }));
      fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Identity provider" });
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "Which workloads" });
      const field = screen.getByLabelText("Subject") as HTMLInputElement;
      // The stem as typed: the `*` belongs to the match kind.
      expect(field.value).toBe("workloads/billing/");
      // Unchanged, so the step is read and passed, not saved.
      expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();

      fireEvent.change(field, { target: { value: "workloads/review/" } });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(mocks.withdrawSubject).toHaveBeenCalledOnce());
      expect(mocks.admitSubject.mock.calls[0]?.[0]?.request).toMatchObject({
        gramProject: "project-one",
        admitWorkloadSubjectForm: {
          projectScoped: false,
          issuer: "https://idp.example",
          subject: "workloads/review/*",
          matchKind: "wildcard",
          agentId: "agent_one",
        },
      });
      expect(mocks.withdrawSubject.mock.calls[0]?.[0]).toEqual({
        request: { id: "admission_custom" },
      });
    });
  });
});

describe("Servers that sign in as a person", () => {
  const linear = {
    id: "server_linear",
    name: "Linear",
    slug: "linear",
    platformEndpointSlug: "linear",
    projectId: "project_one",
    remoteMcpServerId: "remote_linear",
    userSessionIssuerId: "issuer_linear",
    visibility: "private",
    networkAccessMode: "dual",
    createdAt: new Date(),
    updatedAt: new Date(),
  };
  const mine = {
    id: "session_mine",
    remoteSessionClientId: "client_linear",
    userSessionIssuerId: "issuer_linear",
    subjectUrn: "user:user_owner",
    upstreamEmail: "owner@upstream.test",
    accessExpiresAt: new Date(Date.now() + 3_600_000),
    hasRefreshToken: true,
    scopes: [],
    createdAt: new Date(),
    updatedAt: new Date(),
  };

  async function reachServers() {
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Release Bot" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "MCP Selection" });
    await screen.findByRole("checkbox", { name: "Linear" });
  }

  beforeEach(() => {
    mocks.mcpServers.mockResolvedValue({ mcpServers: [linear] });
    mocks.sessionClients.mockResolvedValue([
      { id: "client_linear", credentialOwner: "subject" },
    ]);
    mocks.listDelegableGrants.mockResolvedValue([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_linear",
          projectId: "project_one",
        },
      },
    ]);
  });

  it("uses the creator's own connection, says when it ends, and attaches it before the key", async () => {
    mocks.sessions.mockResolvedValue([mine]);
    await reachServers();

    expect(screen.queryByText(/owner@upstream.test/)).toBeNull();
    fireEvent.click(screen.getByRole("checkbox", { name: "Linear" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "MCP Authentication" });
    expect(
      (await screen.findAllByText(/Your connection|Uses your connection/))
        .length,
    ).toBeGreaterThan(0);
    // Read as the creator, before the agent exists.
    expect(mocks.sessions).toHaveBeenCalledWith(
      expect.objectContaining({
        gramProject: "project-one",
        subjectUrn: "user:user_owner",
      }),
    );
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );

    await waitFor(() => expect(mocks.createKey).toHaveBeenCalledTimes(1));
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants,
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_linear",
          projectId: "project_one",
        },
      },
    ]);
    // The issuer's sign-ins are read in the server's own project, before the
    // agent exists and again while attaching.
    expect(mocks.sessionClients).toHaveBeenCalledWith({
      gramProject: "project-one",
      userSessionIssuerId: "issuer_linear",
    });
    expect(
      mocks.sessionClients.mock.calls.every(
        ([request]) =>
          request.gramProject === "project-one" &&
          request.userSessionIssuerId === "issuer_linear",
      ),
    ).toBe(true);
    // Checked again as the new agent's candidates, then attached.
    expect(mocks.sessions).toHaveBeenCalledWith(
      expect.objectContaining({
        principalId: "agent_new",
        userSessionIssuerId: "issuer_linear",
      }),
    );
    expect(mocks.attach).toHaveBeenCalledWith(
      {
        gramProject: "project-one",
        attachBindingRequestBody: {
          principalId: "agent_new",
          userSessionIssuerId: "issuer_linear",
          remoteSessionId: "session_mine",
        },
      },
      undefined,
      expect.anything(),
    );
    expect(mocks.attach.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.createKey.mock.invocationCallOrder[0]!,
    );
  });

  it("attaches a personal connection before admitting a workload identity", async () => {
    mocks.sessions.mockResolvedValue([mine]);
    mocks.platforms.mockReturnValue([gke]);
    await reachServers();
    fireEvent.click(screen.getByRole("checkbox", { name: "Linear" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(await screen.findByRole("radio", { name: /GKE/ }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your cluster" });
    for (const [label, value] of [
      ["Project ID", "example-project"],
      ["Location", "us-central1"],
      ["Cluster name", "example-cluster"],
    ] as const) {
      fireEvent.change(screen.getByLabelText(label), { target: { value } });
    }
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your workload" });
    fireEvent.change(screen.getByLabelText("Namespace"), {
      target: { value: "example" },
    });
    fireEvent.change(screen.getByLabelText("Service account"), {
      target: { value: "worker" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect GKE" }));
    await waitFor(() => expect(mocks.admitSubject).toHaveBeenCalledOnce());
    expect(mocks.attach).toHaveBeenCalledWith(
      expect.objectContaining({
        attachBindingRequestBody: {
          principalId: "agent_new",
          userSessionIssuerId: "issuer_linear",
          remoteSessionId: "session_mine",
        },
      }),
      undefined,
      expect.anything(),
    );
    expect(mocks.attach.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.admitSubject.mock.invocationCallOrder[0]!,
    );
    expect(mocks.createKey).not.toHaveBeenCalled();
  });

  it("withholds a personal server when its policy grant is refused", async () => {
    mocks.sessions.mockResolvedValue([mine]);
    mocks.checkPolicyGrants.mockImplementation(
      answerChecks({ server_linear: "caller" }),
    );
    await reachServers();

    expect(
      await screen.findByText(
        "You cannot reach this server, so this agent cannot either.",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("checkbox", { name: "Linear" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it.each(["missing", "expired"])(
    "allows selecting a %s connection and resolves it on authentication",
    async (state) => {
      if (state === "expired")
        mocks.sessions.mockResolvedValue([
          { ...mine, hasRefreshToken: false, accessExpiresAt: new Date(0) },
        ]);
      await reachServers();
      const server = screen.getByRole("checkbox", { name: "Linear" });
      await waitFor(() => expect(server.hasAttribute("disabled")).toBe(false));
      fireEvent.click(server);
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      await screen.findByRole("heading", { name: "MCP Authentication" });
      const next = screen.getByRole("button", { name: "Continue" });
      await waitFor(() => expect(next.hasAttribute("disabled")).toBe(true));
      fireEvent.click(next);
      expect(mocks.createAgent).not.toHaveBeenCalled();
      expect(mocks.createKey).not.toHaveBeenCalled();
      const popup = {
        location: { href: "" },
        closed: false,
        opener: window as Window | null,
        focus: vi.fn(),
      };
      const open = vi
        .spyOn(window, "open")
        .mockReturnValue(popup as unknown as Window);
      // The first row starts expanded.
      screen.getByRole("button", {
        name: /Linear.*(?:Expired|Requires connection)/,
      });
      fireEvent.click(
        await screen.findByRole("button", {
          name: state === "expired" ? "Reauthenticate" : "Connect account",
        }),
      );
      expect(open).toHaveBeenCalled();
      const reads = mocks.sessions.mock.calls.length;
      mocks.sessions.mockResolvedValue([mine]);
      popup.closed = true;
      fireEvent(window, new Event("focus"));
      await waitFor(() =>
        expect(mocks.sessions.mock.calls.length).toBeGreaterThan(reads),
      );
      await waitFor(() => expect(next.hasAttribute("disabled")).toBe(false));
      fireEvent.click(next);
      fireEvent.click(
        await screen.findByRole("button", { name: "Create agent" }),
      );
      await waitFor(() => expect(mocks.createKey).toHaveBeenCalledTimes(1));
      expect(mocks.attach).toHaveBeenCalledWith(
        expect.objectContaining({
          attachBindingRequestBody: expect.objectContaining({
            remoteSessionId: mine.id,
          }),
        }),
        undefined,
        expect.anything(),
      );
      open.mockRestore();
    },
  );

  it("removes a selected server from authentication without backtracking", async () => {
    await reachServers();
    fireEvent.click(screen.getByRole("checkbox", { name: "Linear" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "MCP Authentication" });
    fireEvent.click(screen.getByRole("button", { name: "Remove Linear" }));
    expect(
      screen.getByRole("heading", { name: "MCP Authentication" }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Remove Linear" })).toBeNull();
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it("allows All servers through authentication while naming missing connections", async () => {
    await reachServers();
    fireEvent.click(await enabledAllServers());
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "MCP Authentication" });
    expect(
      (await screen.findByText(/No usable connection available for/))
        .textContent,
    ).toContain("Linear");
    await continueFromAuthentication();
    expect(
      await screen.findByRole("button", { name: "Create agent" }),
    ).toBeTruthy();
  });

  it("names the step that failed when the connection cannot be attached", async () => {
    mocks.sessions.mockResolvedValue([mine]);
    mocks.attach.mockRejectedValue(new Error("attachment refused"));
    await reachServers();
    fireEvent.click(screen.getByRole("checkbox", { name: "Linear" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );

    expect((await screen.findByRole("alert")).textContent).toBe(
      "The agent was created, but it could not use your connection. attachment refused",
    );
    expect(mocks.createKey).not.toHaveBeenCalled();
  });
});

const githubActions = {
  key: "github-actions",
  displayName: "GitHub Actions",
  description: "Jobs in one repository.",
  icon: "",
  enabled: true,
  issuer: {
    value: "https://token.actions.githubusercontent.com",
    visibility: "read-only",
  },
  jwksUri: {
    value: "https://token.actions.githubusercontent.com/.well-known/jwks",
    visibility: "hidden",
  },
  variables: [
    ["owner", "Owner", "[A-Za-z0-9][A-Za-z0-9-]{0,38}(?:@[0-9]+)?"],
    ["repo", "Repository", "[A-Za-z0-9._-]{1,100}(?:@[0-9]+)?"],
  ].map(([key, label, pattern]) => ({
    key,
    tier: "rule",
    label,
    help: "",
    placeholder: "",
    pattern,
    patternMessage: "",
  })),
  subject: { template: "repo:{owner}/{repo}:", wildcard: true },
};

describe("Connecting a GitHub repository", () => {
  /** Chooses GitHub Actions for a new agent and fills in the repository. */
  async function reachRepositoryStep() {
    mocks.platforms.mockReturnValue([githubActions]);
    setup();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Release Bot" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(
      await screen.findByRole("radio", { name: /GitHub Actions/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your repository" });
    fireEvent.change(screen.getByLabelText("Owner"), {
      target: { value: "acme" },
    });
    fireEvent.change(screen.getByLabelText("Repository"), {
      target: { value: "payments-api" },
    });
  }

  /** Sets up GitHub Actions for an agent that already exists. */
  async function reachRepositoryStepForExistingAgent() {
    mocks.platforms.mockReturnValue([githubActions]);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <CredentialsWizard
            agent={
              {
                id: "agent_one",
                name: "Release Bot",
                lifecycle: "active",
                projectId: "project_one",
                ownerUserId: "user_owner",
                permissions: { write: true, authorize: true },
              } as never
            }
            initialPlatformKey="github-actions"
            onDone={() => {}}
            verifyPollMs={20}
          />
        </QueryClientProvider>
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByRole("checkbox", { name: "GitHub" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await continueFromAuthentication();
    fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Your repository" });
    fireEvent.change(screen.getByLabelText("Owner"), {
      target: { value: "acme" },
    });
    fireEvent.change(screen.getByLabelText("Repository"), {
      target: { value: "payments-api" },
    });
  }

  it("checks every grant an existing agent lacks before writing any of them", async () => {
    // The agent already polls; it lacks only the hooks grant.
    mocks.listPolicyGrants.mockResolvedValue([
      {
        id: "grant_sync",
        effect: "allow",
        scope: "org:device_agent_sync",
        selector: { resourceKind: "org", resourceId: "*" },
      },
    ]);
    await reachRepositoryStepForExistingAgent();
    fireEvent.click(
      screen.getByRole("button", { name: "Connect GitHub Actions" }),
    );
    await waitFor(() => expect(mocks.createGrant).toHaveBeenCalledTimes(1));
    const lastCheck = mocks.checkPolicyGrants.mock.calls.at(-1)?.[0];
    expect(lastCheck?.checkAgentPolicyGrantsForm).toEqual({
      agentId: "agent_one",
      policyGrants: [
        {
          effect: "allow",
          scope: "org:hooks_ingest",
          selector: {
            resourceKind: "org",
            resourceId: "org_example",
            projectId: "project_one",
          },
        },
      ],
    });
    expect(
      mocks.createGrant.mock.calls[0]?.[0]?.request?.createAgentPolicyGrantForm
        ?.scope,
    ).toBe("org:hooks_ingest");
  });

  it("writes no grant to an existing agent when one of them would be refused", async () => {
    mocks.checkPolicyGrants.mockImplementation(
      (request: {
        checkAgentPolicyGrantsForm: { policyGrants: { scope: string }[] };
      }) =>
        Promise.resolve({
          results: request.checkAgentPolicyGrantsForm.policyGrants.map(
            (grant) =>
              grant.scope === "org:hooks_ingest"
                ? { allowed: false, limitedBy: "owner" }
                : { allowed: true },
          ),
        }),
    );
    await reachRepositoryStepForExistingAgent();
    await waitFor(() => expect(mocks.checkPolicyGrants).toHaveBeenCalled());
    fireEvent.click(
      screen.getByRole("button", { name: "Connect GitHub Actions" }),
    );
    expect(
      await screen.findByText(/owner does not hold org:hooks_ingest/),
    ).toBeTruthy();
    expect(mocks.createGrant).not.toHaveBeenCalled();
    expect(mocks.admitSubject).not.toHaveBeenCalled();
  });

  it("trusts GitHub for the organization and gives the agent what the device agent needs", async () => {
    await reachRepositoryStep();
    expect(screen.getByText("repo:acme/payments-api:*")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Connect GitHub Actions" }),
    );

    await waitFor(() => expect(mocks.admitSubject).toHaveBeenCalledTimes(1));
    // The device-agent endpoint reads organization-tier rules only, so
    // neither write names a project.
    expect(mocks.registerIssuer.mock.calls[0]?.[0]?.request).toEqual({
      registerWorkloadIssuerForm: expect.objectContaining({
        issuer: "https://token.actions.githubusercontent.com",
        jwksUri: "https://token.actions.githubusercontent.com/.well-known/jwks",
      }),
    });
    expect(mocks.admitSubject.mock.calls[0]?.[0]?.request).toEqual({
      admitWorkloadSubjectForm: {
        issuer: "https://token.actions.githubusercontent.com",
        subject: "repo:acme/payments-api:*",
        matchKind: "wildcard",
        agentId: "agent_new",
        tags: [],
      },
    });
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants,
    ).toEqual(
      expect.arrayContaining([
        {
          effect: "allow",
          scope: "org:device_agent_sync",
          selector: { resourceKind: "org", resourceId: "*" },
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
      ]),
    );
    // The hook grant every new agent gets and the one the platform needs are
    // the same grant, sent once.
    expect(
      mocks.createAgent.mock.calls[0]?.[0]?.createAgentForm?.policyGrants.filter(
        (grant: { scope: string }) => grant.scope === "org:hooks_ingest",
      ),
    ).toHaveLength(1);
    await screen.findByRole("heading", { name: "What the job gets" });
  });

  it("registers its own issuer when only a project trusts GitHub, and says that project's servers read the project's", async () => {
    mocks.workloadIssuers.mockReturnValue([
      {
        id: "issuer_project",
        organizationId: "org_example",
        projectId: "project_one",
        name: "Project GitHub",
        issuer: "https://token.actions.githubusercontent.com",
        jwksUri: "https://token.actions.githubusercontent.com/.well-known/jwks",
      },
    ]);
    await reachRepositoryStep();
    expect(screen.getByText(/This project also trusts/)).toBeTruthy();
    // The remedy is withdrawing the project's own issuer, not an agent's
    // access, which would leave the issuer shadowing the rule.
    expect(
      screen.getByText(/withdrawIssuer\?id=\s*issuer_project/),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Connect GitHub Actions" }),
    );
    await waitFor(() => expect(mocks.admitSubject).toHaveBeenCalledTimes(1));
    expect(mocks.registerIssuer).toHaveBeenCalledTimes(1);
  });

  it("creates nothing when the device agent's permissions cannot be given", async () => {
    mocks.checkPolicyGrants.mockImplementation(
      (request: {
        checkAgentPolicyGrantsForm: { policyGrants: { scope: string }[] };
      }) =>
        Promise.resolve({
          results: request.checkAgentPolicyGrantsForm.policyGrants.map(
            (grant) =>
              grant.scope === "org:hooks_ingest"
                ? { allowed: false, limitedBy: "caller" }
                : { allowed: true },
          ),
        }),
    );
    await reachRepositoryStep();
    const connect = screen.getByRole("button", {
      name: "Connect GitHub Actions",
    });
    await waitFor(() => expect(mocks.checkPolicyGrants).toHaveBeenCalled());
    fireEvent.click(connect);
    expect(
      await screen.findByText(/You do not hold org:hooks_ingest/),
    ).toBeTruthy();
    expect(mocks.createAgent).not.toHaveBeenCalled();
    expect(mocks.admitSubject).not.toHaveBeenCalled();
  });

  it("lists the plugins the job receives and the servers it cannot use", async () => {
    const everyone = {
      id: "plugin_ci",
      name: "CI tools",
      assignmentCount: 1,
      agentPluginsV1Compatible: true,
      autoCreated: false,
      createdAt: new Date(0),
    };
    mocks.plugins.mockReturnValue([
      everyone,
      {
        ...everyone,
        id: "plugin_none",
        name: "Unassigned",
        assignmentCount: 0,
      },
    ]);
    mocks.getPlugin.mockResolvedValue({
      ...everyone,
      assignments: [
        {
          id: "a",
          principalUrn: "*",
          installMode: "required",
          createdAt: new Date(0),
        },
      ],
      servers: [
        {
          id: "s1",
          displayName: "GitHub",
          toolsetId: "toolset_one",
          policy: "required",
          sortOrder: 0,
          createdAt: new Date(0),
        },
        {
          id: "s2",
          displayName: "Billing",
          toolsetId: "toolset_billing",
          policy: "required",
          sortOrder: 1,
          createdAt: new Date(0),
        },
      ],
    });
    await reachRepositoryStep();
    fireEvent.click(
      screen.getByRole("button", { name: "Connect GitHub Actions" }),
    );
    await screen.findByRole("heading", { name: "What the job gets" });
    expect(await screen.findByText("CI tools")).toBeTruthy();
    expect(mocks.getPlugin).toHaveBeenCalledTimes(1);
    expect(
      await screen.findByText(/Release Bot cannot use Billing/),
    ).toBeTruthy();
  });

  it("fills the workflow with the organization and the hook events project", async () => {
    mocks.plugins.mockReturnValue([]);
    await reachRepositoryStep();
    fireEvent.click(
      screen.getByRole("button", { name: "Connect GitHub Actions" }),
    );
    await screen.findByRole("heading", { name: "What the job gets" });
    expect(
      screen.getByText(/No plugin in this project is assigned to Everyone/),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const workflow = await screen.findByLabelText(
      ".github/workflows/speakeasy.yml",
    );
    expect(workflow.textContent).toContain("organization-id: org_example");
    expect(workflow.textContent).toContain("project: project-one");
    expect(workflow.textContent).toContain("anthropics/claude-code-action@v1");
    expect(
      screen
        .getByRole("radio", { name: "Claude Code" })
        .getAttribute("aria-checked"),
    ).toBe("true");

    fireEvent.click(screen.getByRole("radio", { name: "Codex" }));
    expect(workflow.textContent).toContain("openai/codex-action@v1");
    expect(workflow.textContent).toContain(
      "openai-api-key: ${{ secrets.OPENAI_API_KEY }}",
    );
    expect(workflow.textContent).toContain("codex-home: ${{ env.CODEX_HOME }}");
    expect(workflow.textContent).not.toContain("anthropics/claude-code-action");
    expect(
      screen.getByRole("link", { name: "Codex action" }).getAttribute("href"),
    ).toBe("https://learn.chatgpt.com/docs/github-action");

    fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }));
    expect(workflow.textContent).toContain("anthropics/claude-code-action@v1");
    expect(workflow.textContent).not.toContain("openai/codex-action");
  });
});

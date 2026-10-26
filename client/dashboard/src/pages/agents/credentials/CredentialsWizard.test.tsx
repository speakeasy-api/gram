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
  platformsStatus: "success" as "success" | "pending" | "error",
  registerIssuer: vi.fn(),
  admitSubject: vi.fn(),
  projectId: "project_one",
  canFederate: true,
  canReadWorkloads: true,
  renameAgent: vi.fn(),
  createGrant: vi.fn(),
  deleteGrant: vi.fn(),
  agentList: vi.fn(),
  sessions: vi.fn(),
  sessionClients: vi.fn(),
  bindings: vi.fn(),
  attach: vi.fn(),
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
      checkPolicyGrants: mocks.checkPolicyGrants,
    },
    keys: { create: mocks.createKey, list: mocks.listKeys },
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
// Reading the workload catalog is its own permission; everything else this
// flow checks follows canFederate.
function hasScope(scope: string): boolean {
  if (scope === "workload:read") return mocks.canReadWorkloads;
  return mocks.canFederate;
}
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: (scopes: string[]) => scopes.every(hasScope),
    hasAnyScope: (scopes: string[]) => scopes.some(hasScope),
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

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.projectId = "project_one";
  mocks.canFederate = true;
  mocks.canReadWorkloads = true;
  mocks.platformsStatus = "success";
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
  mocks.sessions.mockResolvedValue([]);
  mocks.sessionClients.mockResolvedValue([]);
  mocks.bindings.mockResolvedValue([]);
  mocks.attach.mockResolvedValue({});
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
      await screen.findByRole("heading", { name: "Server selection" }),
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
      fireEvent(window, new Event("visibilitychange"));
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
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

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
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

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
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );

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
    const save = screen.getByRole("button", { name: "Save changes and exit" });
    fireEvent.click(save);
    expect(await screen.findByText("Grant is locked")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );
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
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );
    expect(await screen.findByText("Grant is locked")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );
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
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );
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
    fireEvent.click(
      screen.getByRole("button", { name: "Save changes and exit" }),
    );
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

describe("Servers that sign in as a person", () => {
  const linear = {
    id: "server_linear",
    name: "Linear",
    slug: "linear",
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
    await screen.findByRole("heading", { name: "Server selection" });
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

    expect(
      await screen.findByText(
        "Uses your connection: owner@upstream.test · Refreshes itself, no end date",
      ),
    ).toBeTruthy();
    // Read as the creator, before the agent exists.
    expect(mocks.sessions).toHaveBeenCalledWith(
      expect.objectContaining({
        gramProject: "project-one",
        subjectUrn: "user:user_owner",
      }),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "Linear" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
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

  it("keeps a server out of reach until the creator has connected to it", async () => {
    await reachServers();

    expect(
      await screen.findByText(/You have no connection to it yet/),
    ).toBeTruthy();
    expect(
      (screen.getByRole("checkbox", { name: "Linear" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    // Connecting happens on the server's own page, which knows how.
    expect(
      screen
        .getByRole("link", { name: "Connect on its Sessions tab" })
        .getAttribute("href"),
    ).toMatch(/\/mcp\/linear\/sessions$/);
  });

  it("names the step that failed when the connection cannot be attached", async () => {
    mocks.sessions.mockResolvedValue([mine]);
    mocks.attach.mockRejectedValue(new Error("attachment refused"));
    await reachServers();
    await screen.findByText(/^Uses your connection/);
    fireEvent.click(screen.getByRole("checkbox", { name: "Linear" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Create agent" }),
    );

    expect((await screen.findByRole("alert")).textContent).toBe(
      "The agent was created, but it could not use your connection. attachment refused",
    );
    expect(mocks.createKey).not.toHaveBeenCalled();
  });
});

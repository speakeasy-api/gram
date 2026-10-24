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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "All servers" }),
    );

    // The individual rows are not the choice, so counting them would read as
    // "0 of 1 servers selected" beside a toggle that reaches every one.
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "All servers" }),
    );
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "All servers" }),
    );
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "All servers" }),
    );
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "All servers" }),
    );
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
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "All servers" }),
    );
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

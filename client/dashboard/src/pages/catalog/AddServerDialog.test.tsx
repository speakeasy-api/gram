import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render as testingRender,
  act,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { catalogPresetState } from "@/pages/security/server-guardrails/server-guardrail-policy";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AddServerDialog, GuardrailOutcomeNotice } from "./AddServerDialog";
import { readSnowflakeHandoff } from "./snowflake-install";
import { providerProgressKey } from "@/pages/mcp/x/setup/provider-setup";

const mocks = vi.hoisted(() => ({
  getServerDetails: vi.fn(),
  info: vi.fn(),
  providerSetup: "enabled" as "enabled" | "disabled" | "loading",
  hasScope: vi.fn(),
  onOpenChange: vi.fn(),
  reset: vi.fn(),
  startInstall: vi.fn(),
  continueToGuardrails: vi.fn(),
  skip: vi.fn(),
  installWithGuardrail: vi.fn(),
  installForCustomizing: vi.fn(),
  navigate: vi.fn(),
  updateServerConfig: vi.fn(),
  workflow: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { info: mocks.info, error: vi.fn(), warning: vi.fn() },
}));
vi.mock("@/hooks/useProviderSetupRollout", () => ({
  useProviderSetupRollout: () => mocks.providerSetup,
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
  useSession: () => ({ user: { id: "person-1" } }),
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
  useSdkClient: () => ({
    mcpRegistries: { getServerDetails: mocks.getServerDetails },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: mocks.hasScope,
    isLoading: false,
  }),
}));

vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => mocks.navigate,
}));

vi.mock("./useRemoteMcpInstallWorkflow", () => ({
  headerValueKey: (
    serverIndex: number,
    remoteUrl: string,
    headerName: string,
  ) => `${serverIndex}:${remoteUrl}:${headerName}`,
  useRemoteMcpInstallWorkflow: (options: unknown) => mocks.workflow(options),
}));

const server = {
  description: "A test server",
  registryId: "",
  registrySpecifier: "test/server",
  title: "Test Server",
  version: "1.0.0",
  meta: {},
  toolCount: 0,
  isReadOnly: false,
  supportsDcr: true,
  supportsCimd: false,
  remotes: [
    {
      url: "https://mcp.example.test/mcp",
      transportType: "streamable-http" as const,
    },
  ],
};

const salesforceServer = {
  ...server,
  registrySpecifier: "com.salesforce/hosted-mcp",
  title: "Salesforce",
  supportsDcr: false,
  remotes: [
    {
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      transportType: "streamable-http" as const,
    },
    {
      url: "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-all",
      transportType: "streamable-http" as const,
    },
  ],
};

function render(ui: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const wrap = (content: React.ReactElement) => (
    <MemoryRouter>
      <QueryClientProvider client={client}>{content}</QueryClientProvider>
    </MemoryRouter>
  );
  const view = testingRender(wrap(ui));
  return {
    ...view,
    rerender: (content: React.ReactElement) => view.rerender(wrap(content)),
    invalidate,
  };
}

function renderDialog(selectedServer = server): void {
  render(
    <TooltipProvider>
      <AddServerDialog
        servers={[selectedServer]}
        open
        onOpenChange={(open) => {
          mocks.onOpenChange(open);
        }}
      />
    </TooltipProvider>,
  );
}

function mockConfigureWorkflow(selectedServer = server): void {
  mocks.workflow.mockReturnValue({
    phase: "configure",
    projectSlug: "default",
    serverConfigs: [
      {
        server: selectedServer,
        name: "Test Server",
        remotes: server.remotes,
        identityMode: "user",
        agentAuthorization: "",
        headerValues: {},
      },
    ],
    canInstall: true,
    goBack: undefined,
    isServerAlreadyInstalled: () => false,
    reset: mocks.reset,
    startInstall: mocks.startInstall,
    updateServerConfig: mocks.updateServerConfig,
  });
}

beforeEach(() => {
  sessionStorage.clear();
  mocks.providerSetup = "enabled";
  mocks.hasScope.mockReturnValue(false);
  mockConfigureWorkflow();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("Salesforce catalog Add entry", () => {
  function openSalesforce(
    options: Partial<React.ComponentProps<typeof AddServerDialog>> = {},
  ) {
    mockConfigureWorkflow(salesforceServer);
    const element = () => (
      <TooltipProvider>
        <AddServerDialog
          servers={[salesforceServer]}
          open
          openProviderSetup
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
          {...options}
        />
      </TooltipProvider>
    );
    const view = render(element());
    return { ...view, refresh: () => view.rerender(element()) };
  }
  it("enters the existing Identity configuration with one source instead of the endpoint picker", async () => {
    openSalesforce();
    await screen.findByRole("radio", { name: /User Identity/ });
    const options = mocks.workflow.mock.calls.at(-1)![0];
    expect(options.initialConfiguration).toEqual({ identityMode: "user" });
    expect(options.servers[0].remotes).toEqual([salesforceServer.remotes[0]]);
    expect(screen.queryByText("Select endpoints to include")).toBeNull();
    expect(screen.queryByLabelText("Salesforce instance URL")).toBeNull();
  });
  it("returns to guided entry when Manual changes back to User Identity", async () => {
    mocks.hasScope.mockReturnValue(true);
    const view = openSalesforce();
    await screen.findByRole("radio", { name: /User Identity/ });
    fireEvent.click(screen.getByRole("radio", { name: /Manual/ }));
    const configure = mocks.workflow.mock.results.at(-1)!.value;
    configure.serverConfigs[0].identityMode = "none";
    view.refresh();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    const options = mocks.workflow.mock.calls.at(-1)![0];
    expect(options.servers[0].remotes).toEqual([salesforceServer.remotes[0]]);
    expect(options.initialConfiguration).toEqual({
      identityMode: "user",
      name: "Test Server",
    });
  });
  it("keeps the older hosted registry entry on the existing generic configuration path", async () => {
    const hosted = {
      ...salesforceServer,
      registrySpecifier: "com.pulsemcp.mirror/gram-salesforce",
      remotes: [
        {
          url: "https://app.getgram.ai/mcp/speakeasy-team-salesforce",
          transportType: "streamable-http" as const,
          headers: [
            { name: "MCP-Instance-URL", isRequired: true, isSecret: false },
          ],
        },
      ],
    };
    openSalesforce({ servers: [hosted] });
    await screen.findByRole("radio", { name: /User Identity/ });
    expect(
      mocks.workflow.mock.calls.at(-1)![0].servers[0].remotes[0].headers,
    ).toEqual(hosted.remotes[0]!.headers);
    expect(
      mocks.workflow.mock.calls.at(-1)![0].initialConfiguration,
    ).toBeUndefined();
    fireEvent.click(screen.getByRole("radio", { name: /Manual/ }));
    expect(mocks.workflow.mock.calls.at(-1)![0].servers[0].remotes).toEqual(
      hosted.remotes,
    );
  });
  it("updates Manual to Shared credential in place without rebuilding the Salesforce selection", async () => {
    const view = openSalesforce();
    await screen.findByRole("radio", { name: /User Identity/ });
    fireEvent.click(screen.getByRole("radio", { name: /Manual/ }));
    const configure = mocks.workflow.mock.results.at(-1)!.value;
    configure.serverConfigs[0].identityMode = "none";
    view.refresh();
    const initialConfiguration =
      mocks.workflow.mock.calls.at(-1)![0].initialConfiguration;

    fireEvent.click(screen.getByRole("radio", { name: /Shared credential/ }));
    expect(mocks.updateServerConfig).toHaveBeenLastCalledWith(0, {
      identityMode: "agent",
    });
    expect(mocks.workflow.mock.calls.at(-1)![0].initialConfiguration).toBe(
      initialConfiguration,
    );
  });
  it.each(["none", "agent"] as const)(
    "retains the original endpoint flow when Identity changes to %s",
    async (identityMode) => {
      openSalesforce();
      await screen.findByRole("radio", { name: /User Identity/ });
      fireEvent.click(
        screen.getByRole("radio", {
          name: identityMode === "none" ? /Manual/ : /Shared credential/,
        }),
      );
      const options = mocks.workflow.mock.calls.at(-1)![0];
      expect(options.servers[0].remotes).toEqual(salesforceServer.remotes);
      expect(options.initialConfiguration).toEqual({
        identityMode,
        name: "Test Server",
      });
    },
  );
  it.each(["disabled", "loading"] as const)(
    "does not enable guided source selection while rollout is %s",
    async (status) => {
      mocks.providerSetup = status;
      openSalesforce();
      await waitFor(() =>
        expect(mocks.workflow.mock.calls.at(-1)?.[0].servers).toHaveLength(1),
      );
      const options = mocks.workflow.mock.calls.at(-1)![0];
      expect(options.servers[0].remotes).toEqual(salesforceServer.remotes);
      expect(options.initialConfiguration).toBeUndefined();
      if (status === "loading")
        expect(
          screen.queryByRole("radio", { name: /User Identity/ }),
        ).toBeNull();
    },
  );
  it.each([
    { openProviderSetup: false },
    { bulk: true },
    { headless: true },
    { autoStartInstall: true },
  ])("leaves non-guided callers unchanged: %j", async (options) => {
    openSalesforce(options);
    await waitFor(() =>
      expect(mocks.workflow.mock.calls.at(-1)?.[0].servers).toHaveLength(1),
    );
    expect(mocks.workflow.mock.calls.at(-1)![0].servers[0].remotes).toEqual(
      salesforceServer.remotes,
    );
    expect(
      mocks.workflow.mock.calls.at(-1)![0].initialConfiguration,
    ).toBeUndefined();
  });
});

describe("AddServerDialog identity permissions", () => {
  it("marks User Identity as Default for CIMD-only servers", async () => {
    const cimdServer = { ...server, supportsDcr: false, supportsCimd: true };
    mockConfigureWorkflow(cimdServer);
    renderDialog(cimdServer);
    expect(
      await screen.findByRole("radio", { name: /User Identity Default/ }),
    ).toBeDefined();
  });

  it("disables User Identity and explains project:write", async () => {
    renderDialog();

    await waitFor(() =>
      expect(
        screen.getByRole("radio", { name: /User Identity/ }),
      ).toBeDefined(),
    );
    expect(
      (
        screen.getByRole("radio", {
          name: /User Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      screen.getByText(/User Identity creates an authorization server/i),
    ).toBeDefined();
    expect(
      (
        screen.getByRole("button", {
          name: "Add to Project",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(mocks.hasScope).toHaveBeenCalledWith("project:write", "project-1");
  });

  it("allows User Identity with project:write", async () => {
    mocks.hasScope.mockReturnValue(true);
    renderDialog();

    await waitFor(() =>
      expect(
        screen.getByRole("radio", { name: /User Identity/ }),
      ).toBeDefined(),
    );
    expect(
      (
        screen.getByRole("radio", {
          name: /User Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    expect(
      screen.queryByText(/User Identity creates a provider or OAuth client/i),
    ).toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: "Add to Project",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
  });
});

describe("AddServerDialog guardrails", () => {
  it("continues to the guardrails step instead of installing when offered", async () => {
    mocks.hasScope.mockReturnValue(true);
    const configure = mocks.workflow();
    mocks.workflow.mockReturnValue({
      ...configure,
      continueToGuardrails: mocks.continueToGuardrails,
    });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Continue" }));

    expect(mocks.continueToGuardrails).toHaveBeenCalledWith({
      configureSkipped: false,
    });
    expect(mocks.startInstall).not.toHaveBeenCalled();
  });

  function guardrailsPhase() {
    return {
      phase: "guardrails",
      projectSlug: "default",
      guardrail: catalogPresetState([]),
      updateGuardrail: vi.fn(),
      servers: [
        {
          key: "test/server",
          name: "Test Server",
          registrySpecifier: "test/server",
          oauth: true,
        },
      ],
      installWithGuardrail: mocks.installWithGuardrail,
      installForCustomizing: mocks.installForCustomizing,
      skip: mocks.skip,
      goBack: vi.fn(),
      isServerAlreadyInstalled: () => false,
      reset: mocks.reset,
    };
  }

  it("summarizes the recommended guardrail and installs with it", async () => {
    mocks.workflow.mockReturnValue(guardrailsPhase());
    renderDialog();

    expect(await screen.findByText("Guardrails for Test Server")).toBeDefined();
    expect(screen.getByText("OAuth")).toBeDefined();
    expect(
      screen.getByText("Create a risk policy for this server"),
    ).toBeDefined();
    expect(screen.getByText("All tools")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Add to Project" }));
    expect(mocks.installWithGuardrail).toHaveBeenCalledTimes(1);
    expect(mocks.skip).not.toHaveBeenCalled();
  });

  it("logs by default and installs without a policy when switched off", async () => {
    mocks.workflow.mockReturnValue(guardrailsPhase());
    renderDialog();

    expect(await screen.findByText("Log for review · Severity")).toBeDefined();
    fireEvent.click(
      screen.getByRole("switch", { name: "Create a risk policy" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Add to Project" }));
    expect(mocks.skip).toHaveBeenCalledTimes(1);
    expect(mocks.installWithGuardrail).not.toHaveBeenCalled();
  });

  it("reenables customize now when installation is deferred without a server", async () => {
    mocks.installForCustomizing.mockResolvedValue([]);
    mocks.workflow.mockReturnValue(guardrailsPhase());
    renderDialog();
    const button = await screen.findByRole("button", { name: "customize now" });
    fireEvent.click(button);
    await waitFor(() => expect(button).toHaveProperty("disabled", false));
    fireEvent.click(button);
    await waitFor(() =>
      expect(mocks.installForCustomizing).toHaveBeenCalledTimes(2),
    );
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
  it("installs, then opens the full editor pre-filled, on customize now", async () => {
    mocks.installForCustomizing.mockResolvedValue(["srv-1"]);
    mocks.workflow.mockReturnValue(guardrailsPhase());
    renderDialog();

    fireEvent.click(
      await screen.findByRole("button", { name: "customize now" }),
    );

    await waitFor(() => expect(mocks.navigate).toHaveBeenCalledTimes(1));
    const url = new URL(
      mocks.navigate.mock.calls[0]![0] as string,
      "https://x",
    );
    expect(url.pathname).toBe("/acme/projects/default/risk-policies/new");
    expect(url.searchParams.get("kind")).toBe("standard");
    expect(url.searchParams.get("mcp_server_id")).toBe("srv-1");
    expect(url.searchParams.get("action")).toBe("flag");
    expect(url.searchParams.get("name")).toBe("Test Server guardrail");
    expect(mocks.installWithGuardrail).not.toHaveBeenCalled();
  });

  it("says plainly when the guardrail could not be created", () => {
    render(
      <GuardrailOutcomeNotice
        outcome={{
          status: "failed",
          name: "Test Server guardrail",
          error: "policy limit reached",
        }}
      />,
    );

    const alert = screen.getByRole("alert");
    expect(alert.textContent).toContain("Guardrail was not created");
    expect(alert.textContent).toContain("policy limit reached");
  });

  it("confirms a created guardrail", () => {
    render(
      <GuardrailOutcomeNotice
        outcome={{ status: "created", name: "Test Server guardrail" }}
      />,
    );

    expect(screen.getByText("Guardrail created")).toBeDefined();
  });
});

describe("catalog admission", () => {
  const catalogServer = { ...server, registryId: "old-source" };
  function openCatalog() {
    return render(
      <TooltipProvider>
        <AddServerDialog
          servers={[catalogServer]}
          open
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
        />
      </TooltipProvider>,
    );
  }
  it.each(["Selected catalog changed", "Details unavailable"])(
    "blocks before open: %s",
    async (message) => {
      mocks.getServerDetails.mockRejectedValue(new Error(message));
      const { invalidate } = openCatalog();
      await screen.findByText(message);
      expect(invalidate).toHaveBeenCalledWith(
        expect.objectContaining({
          queryKey: ["@gram/client", "mcpRegistries", "listCatalog"],
        }),
      );
      expect(
        screen.queryByRole("button", { name: "Add to Project" }),
      ).toBeNull();
      expect(mocks.startInstall).not.toHaveBeenCalled();
    },
  );
  it("revalidates a source flip while open before accepting installation", async () => {
    mocks.getServerDetails.mockResolvedValue({ remotes: server.remotes });
    openCatalog();
    await screen.findByRole("button", { name: "Add to Project" });
    mocks.getServerDetails.mockRejectedValue(
      new Error("Selected catalog changed"),
    );
    const options = mocks.workflow.mock.calls.at(-1)![0];
    await act(async () => {
      expect(await options.beforeInstall()).toBe(false);
    });
    await screen.findByText("Selected catalog changed");
    expect(mocks.getServerDetails).toHaveBeenCalledTimes(2);
  });
  it("rejects cached endpoints absent from authoritative details", async () => {
    mocks.getServerDetails.mockResolvedValue({ remotes: [] });
    openCatalog();
    await screen.findByText(/Catalog endpoints changed/);
    expect(screen.queryByRole("button", { name: "Add to Project" })).toBeNull();
  });

  it("requires reselection when details change while open", async () => {
    mocks.getServerDetails.mockResolvedValue({ remotes: server.remotes });
    openCatalog();
    await screen.findByRole("button", { name: "Add to Project" });
    mocks.getServerDetails.mockResolvedValue({
      remotes: [
        {
          ...server.remotes[0],
          headers: [{ name: "X-Key", isRequired: true, isSecret: true }],
        },
      ],
    });
    const options = mocks.workflow.mock.calls.at(-1)![0];
    await act(async () => {
      expect(await options.beforeInstall()).toBe(false);
    });
    await screen.findByText(/Catalog details changed/);
  });

  it("does not admit manual URLs through the catalog", async () => {
    renderDialog();
    await screen.findByRole("button", { name: "Add to Project" });
    const options = mocks.workflow.mock.calls.at(-1)![0];
    await act(async () => {
      expect(await options.beforeInstall()).toBe(true);
    });
    expect(mocks.getServerDetails).not.toHaveBeenCalled();
  });
});

describe.each([
  ["Slack", "https://mcp.slack.com/mcp"],
  ["GitHub", "https://api.githubcopilot.com/mcp"],
])("%s catalog setup handoff", (provider, url) => {
  const catalogServer = {
    ...server,
    registrySpecifier: "example/official-mcp",
    title: provider,
    remotes: [
      {
        url,
        transportType: "streamable-http" as const,
      },
    ],
  };
  it("opens the installed server's setup page once from an opted-in catalog install", async () => {
    mocks.workflow.mockReturnValue({
      phase: "complete",
      statuses: [
        {
          key: provider.toLowerCase(),
          name: provider,
          identityMode: "user",
          remoteUrl: url,
          status: "completed",
          mcpServerId: "example-id",
          mcpServerParam: `${provider.toLowerCase()}-installed`,
        },
      ],
      reset: mocks.reset,
    });
    render(
      <TooltipProvider>
        <AddServerDialog
          servers={[catalogServer]}
          open
          openProviderSetup
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
        />
      </TooltipProvider>,
    );
    await waitFor(() => {
      expect(mocks.navigate).toHaveBeenCalledWith(
        `/acme/projects/default/mcp/${provider.toLowerCase()}-installed/setup`,
      );
    });
    expect(mocks.navigate).toHaveBeenCalledTimes(1);
    expect(mocks.onOpenChange).toHaveBeenCalledWith(false);
  });
  it.each(["disabled", "loading"] as const)(
    "keeps the install completion open while rollout is %s",
    async (status) => {
      mocks.providerSetup = status;
      mocks.workflow.mockReturnValue({
        phase: "complete",
        statuses: [
          {
            key: provider.toLowerCase(),
            name: provider,
            identityMode: "user",
            remoteUrl: url,
            status: "completed",
            mcpServerId: "example-id",
            mcpServerParam: "installed",
          },
        ],
        reset: mocks.reset,
      });
      const content = () => (
        <TooltipProvider>
          <AddServerDialog
            servers={[catalogServer]}
            open
            openProviderSetup
            onOpenChange={(open) => {
              mocks.onOpenChange(open);
            }}
          />
        </TooltipProvider>
      );
      const view = render(content());
      await waitFor(() =>
        expect(screen.getByText("Added to your project")).toBeDefined(),
      );
      expect(mocks.navigate).not.toHaveBeenCalled();
      expect(mocks.onOpenChange).not.toHaveBeenCalled();
      if (status === "loading") {
        sessionStorage.clear();
        mocks.providerSetup = "enabled";
        view.rerender(content());
        await waitFor(() => expect(mocks.navigate).toHaveBeenCalledTimes(1));
        expect(mocks.navigate).toHaveBeenCalledWith(
          "/acme/projects/default/mcp/installed/setup",
        );
      }
    },
  );
  it("does not redirect when catalog setup opt-in is absent", async () => {
    mocks.workflow.mockReturnValue({
      phase: "complete",
      statuses: [
        {
          key: provider.toLowerCase(),
          name: provider,
          identityMode: "user",
          remoteUrl: url,
          status: "completed",
          mcpServerId: "example-id",
          mcpServerParam: `${provider.toLowerCase()}-installed`,
        },
      ],
      reset: mocks.reset,
    });
    render(
      <TooltipProvider>
        <AddServerDialog
          servers={[catalogServer]}
          open
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
        />
      </TooltipProvider>,
    );
    await waitFor(() => {
      expect(screen.getByText("Added to your project")).toBeDefined();
    });
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
});

it.each([{ headless: true }, { autoStartInstall: true }])(
  "does not redirect automatic/headless callers even with catalog opt-in: %j",
  async (props) => {
    mocks.workflow.mockReturnValue({
      phase: "complete",
      statuses: [
        {
          key: "slack",
          name: "Slack",
          identityMode: "user",
          remoteUrl: "https://mcp.slack.com/mcp",
          status: "completed",
          mcpServerId: "id",
          mcpServerParam: "slack-installed",
        },
      ],
      reset: mocks.reset,
    });
    render(
      <TooltipProvider>
        <AddServerDialog
          servers={[
            {
              ...server,
              remotes: [
                {
                  url: "https://mcp.slack.com/mcp",
                  transportType: "streamable-http",
                },
              ],
            },
          ]}
          open
          openProviderSetup
          {...props}
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
        />
      </TooltipProvider>,
    );
    await waitFor(() => expect(mocks.workflow).toHaveBeenCalled());
    expect(mocks.navigate).not.toHaveBeenCalled();
  },
);

describe("Snowflake keeps the existing identity modal", () => {
  const progressKey = providerProgressKey(
    "snowflake",
    "person-1",
    "org-1",
    "project-1",
    "catalog:com.snowflake/cortex-mcp",
  );
  const snowflake = {
    ...server,
    registrySpecifier: "com.snowflake/cortex-mcp",
    title: "Snowflake",
    supportsDcr: false,
    remotes: [
      {
        transportType: "streamable-http" as const,
        url: "https://{account_url}/api/v2/databases/{mcp_database}/schemas/{mcp_schema}/mcp-servers/{mcp_server_name}",
      },
    ],
  };
  function configure(identityMode: "user" | "agent" | "none") {
    mocks.hasScope.mockReturnValue(true);
    mockConfigureWorkflow(snowflake);
    const state = mocks.workflow();
    state.serverConfigs[0].identityMode = identityMode;
    state.serverConfigs[0].name = "Snowflake connection";
    mocks.startInstall.mockImplementation(async () => {
      const options = mocks.workflow.mock.calls.at(-1)![0];
      options.deferSetup(state.serverConfigs, {});
    });
    return state;
  }
  function show(
    props: { openProviderSetup?: boolean; attachToGatewayId?: string } = {},
  ) {
    return render(
      <TooltipProvider>
        <AddServerDialog
          servers={[snowflake]}
          open
          openProviderSetup
          {...props}
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
        />
      </TooltipProvider>,
    );
  }
  afterEach(() => {
    mocks.startInstall.mockReset();
  });
  it("shows the existing modal with identity choices and no Snowflake connection fields", async () => {
    configure("none");
    show();
    await screen.findByRole("radio", { name: /User Identity/ });
    expect(
      screen.getByRole("radio", { name: /Shared credential/ }),
    ).toBeDefined();
    expect(screen.getByRole("radio", { name: /Manual/ })).toBeDefined();
    expect(screen.queryByLabelText(/Public.*hostname/)).toBeNull();
    expect(screen.queryByLabelText(/MCP database/)).toBeNull();
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
  it("opens setup only when the User Identity install action is submitted, retaining the name", async () => {
    configure("user");
    show();
    const button = await screen.findByRole("button", {
      name: "Add to Project",
    });
    expect(mocks.navigate).not.toHaveBeenCalled();
    fireEvent.click(button);
    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith(
        "/acme/projects/default/mcp/catalog/com.snowflake%2Fcortex-mcp/setup",
      ),
    );
    expect(mocks.onOpenChange).toHaveBeenCalledWith(false);
    const saved = readSnowflakeHandoff(progressKey);
    expect(saved).toMatchObject({
      name: "Snowflake connection",
    });
  });
  it.each(["none", "agent"] as const)(
    "deferred-setup callback rejects %s identity",
    async (mode) => {
      const state = configure("none");
      show();
      await screen.findByRole("radio", { name: /User Identity/ });
      const options = mocks.workflow.mock.calls.at(-1)![0];
      expect(
        options.deferSetup(
          [{ ...state.serverConfigs[0], identityMode: mode }],
          {},
        ),
      ).toBe(false);
      expect(mocks.navigate).not.toHaveBeenCalled();
    },
  );

  it("preserves a gateway-originated User Identity setup instead of installing its template", async () => {
    configure("user");
    show({ openProviderSetup: false, attachToGatewayId: "gateway-id" });
    fireEvent.click(
      await screen.findByRole("button", { name: "Add to Project" }),
    );
    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith(
        "/acme/projects/default/mcp/catalog/com.snowflake%2Fcortex-mcp/setup?attachToGateway=gateway-id",
      ),
    );
  });
  it("retains the existing optional guardrail selection in the deferred handoff", async () => {
    const state = configure("user");
    show();
    await screen.findByRole("radio", { name: /User Identity/ });
    const options = mocks.workflow.mock.calls.at(-1)![0];
    expect(
      options.deferSetup(state.serverConfigs, {
        guardrail: catalogPresetState([]),
      }),
    ).toBe(true);
    const saved = readSnowflakeHandoff(progressKey);
    expect(saved?.guardrail?.categories).toEqual(new Set(["secrets", "pii"]));
  });
  it.each(["loading", "disabled"] as const)(
    "never opens setup while rollout is %s",
    async (rollout) => {
      const state = configure("user");
      mocks.providerSetup = rollout;
      show();
      await screen.findByRole("radio", { name: /User Identity/ });
      const options = mocks.workflow.mock.calls.at(-1)![0];
      expect(options.deferSetup(state.serverConfigs, {})).toBe(
        rollout === "loading",
      );
      expect(mocks.navigate).not.toHaveBeenCalled();
      if (rollout === "loading")
        expect(mocks.info).toHaveBeenCalledWith(
          "Snowflake setup is loading. Try again in a moment.",
        );
    },
  );
});

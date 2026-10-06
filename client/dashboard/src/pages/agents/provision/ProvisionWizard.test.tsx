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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPurpose } from "./device-agent";
import { ProvisionWizard } from "./ProvisionWizard";

const mocks = vi.hoisted(() => ({
  createAgent: vi.fn(),
  createKey: vi.fn(),
  listKeys: vi.fn(),
  listDelegableGrants: vi.fn(),
  listPolicyGrants: vi.fn(),
  mcpServers: vi.fn(),
  toolsets: vi.fn(),
  fetch: vi.fn(),
  projectId: "project_one",
  isOrgAdmin: true,
  deviceAgentEnabled: true,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope === "org:admin" && mocks.isOrgAdmin,
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({
    status: mocks.deviceAgentEnabled ? "enabled" : "disabled",
  }),
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
      listPolicyGrants: mocks.listPolicyGrants,
    },
    keys: { create: mocks.createKey, list: mocks.listKeys },
  }),
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
// A deployment serves the gateway over HTTPS; jsdom's origin is plain HTTP.
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://gram.example.test",
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

function setup(initialPurpose?: AgentPurpose, agent?: ManagedAgent) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onDone = vi.fn();
  const view = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <ProvisionWizard
          initialPurpose={initialPurpose}
          agent={agent}
          onDone={(id) => {
            onDone(id);
          }}
        />
      </TooltipProvider>
    </QueryClientProvider>,
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
  mocks.isOrgAdmin = true;
  mocks.deviceAgentEnabled = true;
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

    expect(
      await screen.findByText(/curl -fsSL .*\/agent-mcp\/install\/setup_code/),
    ).toBeTruthy();
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

  it(
    "does not count the dashboard's own use of the key as the first call",
    { timeout: 15000 },
    async () => {
      // Minting the setup command authenticates with the key, so the key's
      // access time moves before any runtime has called.
      mocks.listKeys.mockResolvedValue({
        keys: [
          { id: "key_new", lastAccessedAt: new Date(Date.now() - 60_000) },
        ],
      });
      fireEvent.click(await reachCredentialStep());
      fireEvent.click(
        await screen.findByRole("button", { name: "Continue to verification" }),
      );

      expect(
        await screen.findByText("Waiting for the first call"),
      ).toBeTruthy();

      mocks.listKeys.mockResolvedValue({
        keys: [
          { id: "key_new", lastAccessedAt: new Date(Date.now() + 60_000) },
        ],
      });
      expect(
        await screen.findByText("Connected", {}, { timeout: 8000 }),
      ).toBeTruthy();
    },
  );
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

const deviceAgentGrants = [
  {
    effect: "allow",
    scope: "org:device_agent_sync",
    selector: { resourceKind: "org", resourceId: "*" },
  },
  {
    effect: "allow",
    scope: "org:hooks_ingest",
    selector: { resourceKind: "org", resourceId: "*" },
  },
  {
    effect: "allow",
    scope: "project:read",
    selector: { resourceKind: "project", resourceId: "project_one" },
  },
];

async function reachDeviceAgentCredentialStep() {
  setup("device-agent");
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "CI host" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("heading", { name: "Project" });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  return screen.findByRole("button", { name: "Create agent" });
}

/** The install-code request bodies the wizard sent, in order. */
function installRequests(): unknown[] {
  return mocks.fetch.mock.calls.map(([, init]) =>
    JSON.parse((init as RequestInit).body as string),
  );
}

describe("Provisioning a device agent", () => {
  beforeEach(() => {
    mocks.listDelegableGrants.mockResolvedValue(deviceAgentGrants);
  });

  it("creates an organization-wide agent with only the device agent grants", async () => {
    fireEvent.click(await reachDeviceAgentCredentialStep());

    await waitFor(() => expect(mocks.createKey).toHaveBeenCalledTimes(1));
    const form = mocks.createAgent.mock.calls[0]![0].createAgentForm;
    expect(form.policyGrants).toEqual(deviceAgentGrants);
    expect("projectId" in form).toBe(false);
    // The key carries exactly those grants, never mcp:connect.
    expect(
      mocks.createKey.mock.calls[0]![0].createKeyForm.requestedGrants,
    ).toEqual(deviceAgentGrants);
    expect(installRequests()).toEqual([
      { flavor: "device_agent", mode: "ephemeral" },
    ]);
    expect(
      await screen.findByText(
        /^curl -fsSL .*\/agent-mcp\/install\/setup_code \| sh$/,
      ),
    ).toBeTruthy();
    // The review-first form spends the same code without piping it to a shell.
    expect(
      screen.getByText(
        /setup_code' -o "\$f"; then echo "Saved to \$f"; else rm -f "\$f"; fi$/,
      ),
    ).toBeTruthy();
  });

  it("mints a new code when the run mode changes", async () => {
    fireEvent.click(await reachDeviceAgentCredentialStep());
    await screen.findByText(/install\/setup_code \| sh$/);

    fireEvent.click(screen.getByRole("button", { name: "Persistent" }));

    await waitFor(() =>
      expect(installRequests()).toEqual([
        { flavor: "device_agent", mode: "ephemeral" },
        { flavor: "device_agent", mode: "service" },
      ]),
    );
  });

  it("says which scope cannot be delegated instead of issuing a narrower key", async () => {
    mocks.listDelegableGrants.mockResolvedValue([deviceAgentGrants[2]]);
    fireEvent.click(await reachDeviceAgentCredentialStep());

    expect((await screen.findByRole("alert")).textContent).toContain(
      "org:admin",
    );
    expect(mocks.createKey).not.toHaveBeenCalled();
  });

  it("is not offered without org:admin", () => {
    mocks.isOrgAdmin = false;
    setup("device-agent");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "CI host" },
    });

    expect(
      screen
        .getByRole("radio", { name: /Device agent/ })
        .getAttribute("data-disabled"),
    ).not.toBeNull();
    expect(
      (screen.getByRole("button", { name: "Continue" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("is not offered where the device agent is disabled", () => {
    mocks.deviceAgentEnabled = false;
    setup();

    expect(
      screen
        .getByRole("radio", { name: /Device agent/ })
        .getAttribute("data-disabled"),
    ).not.toBeNull();
  });
});

const existingAgent = {
  id: "agent_existing",
  name: "CI host",
  lifecycle: "active",
  permissions: { read: true, write: true, authorize: true },
} as unknown as ManagedAgent;

describe("Issuing a key to an existing device agent", () => {
  beforeEach(() => {
    mocks.listDelegableGrants.mockResolvedValue(deviceAgentGrants);
    mocks.listPolicyGrants.mockResolvedValue(
      deviceAgentGrants.map((g, i) => ({ id: `grant_${i}`, ...g })),
    );
  });

  it("reads the purpose from the agent's policy and locks it", async () => {
    setup(undefined, existingAgent);

    await waitFor(() =>
      expect(
        screen
          .getByRole("radio", { name: /Device agent/ })
          .getAttribute("data-state"),
      ).toBe("checked"),
    );
    expect(
      screen
        .getByRole("radio", { name: /MCP servers/ })
        .getAttribute("data-disabled"),
    ).not.toBeNull();
  });

  it("still requires org:admin, as creating one does", async () => {
    mocks.isOrgAdmin = false;
    setup(undefined, existingAgent);

    expect(
      await screen.findByText(
        "Provisioning a device agent requires org:admin.",
      ),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Continue" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("says when the agent's permissions cannot be read, and retries", async () => {
    mocks.listPolicyGrants.mockRejectedValueOnce(new Error("unavailable"));
    setup(undefined, existingAgent);

    expect((await screen.findByRole("alert")).textContent).toContain(
      "Could not read this agent's permissions.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(mocks.listPolicyGrants).toHaveBeenCalledTimes(2);
  });
});

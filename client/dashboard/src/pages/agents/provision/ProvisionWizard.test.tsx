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
import { ProvisionWizard } from "./ProvisionWizard";

const mocks = vi.hoisted(() => ({
  createAgent: vi.fn(),
  createKey: vi.fn(),
  listKeys: vi.fn(),
  listDelegableGrants: vi.fn(),
  mcpServers: vi.fn(),
  toolsets: vi.fn(),
  fetch: vi.fn(),
  projectId: "project_one",
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

function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onDone = vi.fn();
  const view = render(
    <QueryClientProvider client={client}>
      <ProvisionWizard
        onDone={(id) => {
          onDone(id);
        }}
      />
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

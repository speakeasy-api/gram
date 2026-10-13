import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { ServerAttachment } from "./ServerAttachment";
import { attachableServers, policyGrantsConnect } from "./attachment";

const state = vi.hoisted(() => ({
  servers: [] as unknown[],
  grants: [] as unknown[],
  grantsError: false,
  canWrite: true,
  get: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: false,
    hasScope: () => state.canWrite,
    hasAllScopes: () => state.canWrite,
    hasAnyScope: () => state.canWrite,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    mcpServers: { get: state.get, update: state.update },
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: ({ projectSlug }: { projectSlug?: string }) => ({
    agents: { href: () => `/acme/projects/${projectSlug}/agent-management` },
  }),
}));
vi.mock("@gram/client/react-query/listMcpServersForOrg.js", () => ({
  useListMcpServersForOrg: () => ({
    data: { mcpServers: state.servers },
    isPending: false,
    isError: false,
  }),
  invalidateAllListMcpServersForOrg: vi.fn(),
}));
vi.mock("@gram/client/react-query/listProjects.js", () => ({
  useListProjects: () => ({
    data: {
      projects: [
        { id: "proj-a", slug: "alpha", name: "Alpha" },
        { id: "proj-b", slug: "beta", name: "Beta" },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  invalidateAllMcpServers: vi.fn(),
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  invalidateAllGetMcpServer: vi.fn(),
}));
vi.mock("@gram/client/react-query/agentsListPolicyGrants.js", () => ({
  useAgentsListPolicyGrants: () => ({
    data: state.grants,
    isPending: false,
    isError: state.grantsError,
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(() => {
  cleanup();
  state.servers = [];
  state.grants = [];
  state.grantsError = false;
  state.canWrite = true;
  state.get.mockReset();
  state.update.mockReset();
});

function server(overrides: Partial<McpServer> & { id: string }): McpServer {
  return {
    createdAt: new Date(0),
    updatedAt: new Date(0),
    projectId: "proj-a",
    name: overrides.id,
    slug: overrides.id,
    visibility: "private",
    networkAccessMode: "public_only",
    ...overrides,
  } as McpServer;
}

type Props = Parameters<typeof ServerAttachment>[0];

function block(props: Partial<Props>): JSX.Element {
  return (
    <ServerAttachment
      issuerId="issuer-1"
      issuerProjectId=""
      agentId="agent-1"
      agentName="Support bot"
      {...props}
    />
  );
}

function renderBlock(props: Partial<Props> = {}) {
  const queryClient = new QueryClient();
  const wrap = (element: JSX.Element) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <MemoryRouter>{element}</MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>
  );
  const result = render(wrap(block(props)));
  return {
    ...result,
    rerenderWith: (next: Partial<Props>) => result.rerender(wrap(block(next))),
  };
}

function choose(serverId: string) {
  fireEvent.click(
    screen.getByRole("combobox", { name: "Attach an MCP server" }),
  );
  fireEvent.click(screen.getByRole("option", { name: serverId }));
}

it("shows nothing until an issuer is selected", () => {
  state.servers = [server({ id: "support" })];
  renderBlock({ issuerId: "" });
  expect(screen.queryByText("Attach an MCP server")).toBeNull();
});

it("lists the servers already attached to the issuer", () => {
  state.servers = [
    server({ id: "support", userSessionIssuerId: "issuer-1" }),
    server({ id: "ops" }),
  ];
  renderBlock();
  expect(screen.getByText("support (Alpha)")).toBeTruthy();
});

it("attaches a server by updating its latest record under its own project", async () => {
  state.servers = [server({ id: "ops", projectId: "proj-b" })];
  state.get.mockResolvedValue({
    ...server({ id: "ops", projectId: "proj-b" }),
    environmentId: "env-1",
    toolsetId: "toolset-1",
  });
  state.update.mockResolvedValue({});
  renderBlock();
  choose("ops");
  fireEvent.click(screen.getByRole("button", { name: "Attach server" }));
  await waitFor(() => expect(state.update).toHaveBeenCalled());
  expect(state.get).toHaveBeenCalledWith({ id: "ops", gramProject: "beta" });
  expect(state.update).toHaveBeenCalledWith({
    gramProject: "beta",
    updateMcpServerForm: expect.objectContaining({
      id: "ops",
      environmentId: "env-1",
      toolsetId: "toolset-1",
      userSessionIssuerId: "issuer-1",
      visibility: "private",
    }),
  });
});

it("warns before moving a server off another authorization server", async () => {
  state.servers = [server({ id: "ops", userSessionIssuerId: "issuer-0" })];
  state.get.mockResolvedValue(server({ id: "ops" }));
  state.update.mockResolvedValue({});
  renderBlock();
  choose("ops");
  fireEvent.click(screen.getByRole("button", { name: "Attach server" }));
  expect(state.update).not.toHaveBeenCalled();
  expect(
    screen.getByText("Change this server's authorization server?"),
  ).toBeTruthy();
  expect(screen.getByText(/must sign in again/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Change and attach" }));
  await waitFor(() => expect(state.update).toHaveBeenCalled());
});

it("says when the chosen server is already attached", () => {
  state.servers = [server({ id: "ops", userSessionIssuerId: "issuer-1" })];
  renderBlock();
  choose("ops");
  expect(
    screen.getByText("Already attached to this authorization server."),
  ).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Attach server" })).toBeNull();
});

it("surfaces a failed attachment and keeps the server chosen", async () => {
  state.servers = [server({ id: "ops" })];
  state.get.mockRejectedValue(new Error("ECONNRESET at 10.0.0.1"));
  renderBlock();
  choose("ops");
  fireEvent.click(screen.getByRole("button", { name: "Attach server" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.getByText("Failed to attach the server")).toBeTruthy();
  expect(screen.queryByText(/ECONNRESET/)).toBeNull();
  expect(screen.getByRole("button", { name: "Attach server" })).toBeTruthy();
});

it("points at the agent's policy when no direct grant for the server is shown", () => {
  state.servers = [server({ id: "ops", projectId: "proj-b" })];
  renderBlock();
  choose("ops");
  expect(
    screen.getByText(/No direct mcp:connect grant for ops is shown in/),
  ).toBeTruthy();
  expect(screen.getByText(/the server decides effective access/)).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: "Support bot's policy" })
      .getAttribute("href"),
  ).toBe("/acme/projects/beta/agent-management?id=agent-1");
});

it("never turns a missing direct grant into a refusal verdict", () => {
  // Only a broader scope is configured; the server may still admit through
  // what that implies, so the step must not say the session is refused.
  state.servers = [server({ id: "ops" })];
  state.grants = [
    {
      id: "g1",
      scope: "mcp:read",
      effect: "allow",
      selector: { resourceKind: "mcp", resourceId: "*" },
    },
  ];
  renderBlock();
  choose("ops");
  expect(screen.getByText(/No direct mcp:connect grant/)).toBeTruthy();
  expect(screen.queryByText(/refused/)).toBeNull();
  expect(screen.queryByText(/may connect/)).toBeNull();
  expect(
    screen.getByRole("link", { name: "Support bot's policy" }),
  ).toBeTruthy();
});

it("describes a matching grant as policy to review, not as admitted access", () => {
  state.servers = [server({ id: "ops" })];
  state.grants = [
    {
      id: "g1",
      scope: "mcp:connect",
      effect: "allow",
      selector: { resourceKind: "mcp", resourceId: "*", projectId: "proj-a" },
    },
  ];
  renderBlock();
  choose("ops");
  expect(
    screen.getByText(/has a grant that allows connecting to ops/),
  ).toBeTruthy();
  expect(
    screen.getByText(/checks the whole policy on every connection/),
  ).toBeTruthy();
  expect(screen.queryByText(/may connect/)).toBeNull();
  // The policy stays one click away even when a grant was found: the grant
  // is where to look, not proof the server will admit the session.
  expect(
    screen
      .getByRole("link", { name: "Support bot's policy" })
      .getAttribute("href"),
  ).toBe("/acme/projects/alpha/agent-management?id=agent-1");
});

it("keeps the policy link when the agent's policy cannot be read", () => {
  state.servers = [server({ id: "ops" })];
  state.grantsError = true;
  renderBlock();
  choose("ops");
  expect(screen.getByText(/could not be read/)).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Support bot's policy" }),
  ).toBeTruthy();
});

it("leaves out servers a platform token cannot reach", () => {
  const servers = attachableServers(
    [
      server({ id: "on" }),
      server({ id: "off", visibility: "disabled" }),
      server({ id: "private", networkAccessMode: "private_only" }),
    ],
    "",
  );
  expect(servers.map((s) => s.id)).toEqual(["on"]);
});

it("lets an organization issuer span projects but keeps a project issuer in its own", () => {
  const all = [
    server({ id: "a1", projectId: "proj-a" }),
    server({ id: "b1", projectId: "proj-b" }),
  ];
  expect(attachableServers(all, "").map((s) => s.id)).toEqual(["a1", "b1"]);
  expect(attachableServers(all, "proj-a").map((s) => s.id)).toEqual(["a1"]);
  expect(attachableServers(all, "proj-c")).toEqual([]);
});

it("offers a project issuer only the servers of its own project", () => {
  state.servers = [
    server({ id: "a1", projectId: "proj-a" }),
    server({ id: "b1", projectId: "proj-b" }),
  ];
  renderBlock({ issuerId: "issuer-a", issuerProjectId: "proj-a" });
  fireEvent.click(
    screen.getByRole("combobox", { name: "Attach an MCP server" }),
  );
  expect(screen.getByRole("option", { name: "a1" })).toBeTruthy();
  expect(screen.queryByRole("option", { name: "b1" })).toBeNull();
});

it("drops the chosen server and any pending confirmation when the issuer changes", async () => {
  state.servers = [
    server({ id: "a1", projectId: "proj-a", userSessionIssuerId: "issuer-0" }),
    server({ id: "b1", projectId: "proj-b" }),
  ];
  state.get.mockResolvedValue(server({ id: "b1", projectId: "proj-b" }));
  state.update.mockResolvedValue({});
  const view = renderBlock({ issuerId: "issuer-1", issuerProjectId: "" });
  choose("a1");
  fireEvent.click(screen.getByRole("button", { name: "Attach server" }));
  expect(
    screen.getByText("Change this server's authorization server?"),
  ).toBeTruthy();

  view.rerenderWith({ issuerId: "issuer-b", issuerProjectId: "proj-b" });
  expect(
    screen.queryByText("Change this server's authorization server?"),
  ).toBeNull();
  expect(screen.queryByRole("button", { name: "Attach server" })).toBeNull();
  expect(state.update).not.toHaveBeenCalled();

  // A fresh choice under the new issuer attaches to that issuer, not the
  // one the earlier confirmation was for.
  choose("b1");
  fireEvent.click(screen.getByRole("button", { name: "Attach server" }));
  await waitFor(() => expect(state.update).toHaveBeenCalled());
  expect(state.update).toHaveBeenCalledWith({
    gramProject: "beta",
    updateMcpServerForm: expect.objectContaining({
      id: "b1",
      userSessionIssuerId: "issuer-b",
    }),
  });
});

it("drops a selection and confirmation for good when the issuer changes and comes back", () => {
  state.servers = [
    server({ id: "a1", projectId: "proj-a", userSessionIssuerId: "issuer-0" }),
  ];
  const view = renderBlock({ issuerId: "issuer-1", issuerProjectId: "" });
  choose("a1");
  fireEvent.click(screen.getByRole("button", { name: "Attach server" }));
  expect(
    screen.getByText("Change this server's authorization server?"),
  ).toBeTruthy();

  view.rerenderWith({ issuerId: "issuer-b", issuerProjectId: "" });
  view.rerenderWith({ issuerId: "issuer-1", issuerProjectId: "" });
  expect(
    screen.queryByText("Change this server's authorization server?"),
  ).toBeNull();
  expect(screen.queryByRole("button", { name: "Attach server" })).toBeNull();
  expect(screen.getByText("Select an MCP server")).toBeTruthy();
  expect(state.get).not.toHaveBeenCalled();
  expect(state.update).not.toHaveBeenCalled();
});

it("shows a server's own refusal when the attachment update is refused", async () => {
  state.servers = [server({ id: "ops" })];
  state.get.mockResolvedValue(server({ id: "ops" }));
  state.update.mockRejectedValue(
    new ServiceError(
      {
        fault: false,
        id: "error",
        message: "mcp server slug already in use",
        name: "conflict",
        temporary: false,
        timeout: false,
      },
      {
        request: new Request("https://platform.example"),
        response: new Response(null, { status: 409 }),
        body: "",
      },
    ),
  );
  renderBlock();
  choose("ops");
  fireEvent.click(screen.getByRole("button", { name: "Attach server" }));
  expect(
    await screen.findByText("mcp server slug already in use"),
  ).toBeTruthy();
  expect(state.update).toHaveBeenCalledWith({
    gramProject: "alpha",
    updateMcpServerForm: expect.objectContaining({
      id: "ops",
      userSessionIssuerId: "issuer-1",
    }),
  });
  expect(screen.queryByText(/Reload the page/)).toBeNull();
  expect(screen.queryByText(/organization administrator/)).toBeNull();
});

it("keeps the attach button disabled without mcp:write on the server", () => {
  state.servers = [server({ id: "ops" })];
  state.canWrite = false;
  renderBlock();
  choose("ops");
  const button = screen.getByRole("button", { name: "Attach server" });
  expect(button.hasAttribute("disabled")).toBe(true);
  fireEvent.click(button);
  expect(state.get).not.toHaveBeenCalled();
  expect(state.update).not.toHaveBeenCalled();
});

it("reads only an allow grant naming the server's own id or every server", () => {
  const target = server({ id: "srv", toolsetId: "ts", projectId: "proj-a" });
  const grant = (
    resourceId: string,
    projectId?: string,
    extra: Partial<AgentPolicyGrant> = {},
  ): AgentPolicyGrant =>
    ({
      id: resourceId,
      scope: "mcp:connect",
      effect: "allow",
      selector: { resourceKind: "mcp", resourceId, projectId },
      ...extra,
    }) as AgentPolicyGrant;
  expect(policyGrantsConnect([grant("srv")], target)).toBe(true);
  expect(policyGrantsConnect([grant("*", "proj-a")], target)).toBe(true);
  expect(policyGrantsConnect([grant("*", "*")], target)).toBe(true);
  expect(policyGrantsConnect([grant("*", "proj-b")], target)).toBe(false);
  // The toolset id names the dashboard's mcp:write resource, not the
  // runtime's connect check.
  expect(policyGrantsConnect([grant("ts")], target)).toBe(false);
  expect(policyGrantsConnect([grant("other")], target)).toBe(false);
  // Broader scopes and other effects are not read as a connect allow: the
  // server, not this check, decides what they imply.
  expect(
    policyGrantsConnect([{ ...grant("*"), scope: "mcp:read" }], target),
  ).toBe(false);
  expect(
    policyGrantsConnect(
      [{ ...grant("*"), effect: "deny" as AgentPolicyGrant["effect"] }],
      target,
    ),
  ).toBe(false);
  // A narrower grant beside a matching one still reads as "a grant exists":
  // the restriction is for the policy page to show, which is why the UI
  // always links there.
  expect(
    policyGrantsConnect(
      [grant("srv"), { ...grant("*"), scope: "mcp:read" }],
      target,
    ),
  ).toBe(true);
});

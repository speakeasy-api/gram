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
import { TooltipProvider } from "@/components/ui/Tooltip";
import { AgentCredentialAccess } from "./AgentCredentialAccess";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  remove: vi.fn(),
  blockOnConnection: vi.fn(),
  listSessions: vi.fn(),
  blocks: [] as unknown[],
  sessions: [] as unknown[],
  clientPages: [] as unknown[][],
  canReadOrg: false,
}));
function pages(items: unknown[][]) {
  return Promise.resolve(
    (async function* () {
      for (const page of items) yield { result: { items: page } };
    })(),
  );
}
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => mocks.canReadOrg,
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "user_example" } }),
  useOrganization: () => ({ id: "org_example", projects: [] }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    agents: {
      listPolicyGrants: mocks.list,
      createPolicyGrant: mocks.create,
      deletePolicyGrant: mocks.remove,
      listConnectionBlocks: () => pages([mocks.blocks]),
    },
    authClients: { list: () => pages(mocks.clientPages) },
    remoteSessions: { list: mocks.listSessions },
  }),
}));
vi.mock("@gram/client/react-query/agentConnectionBlocks.js", () => ({
  invalidateAllAgentConnectionBlocks: () => Promise.resolve(),
}));
vi.mock("@gram/client/react-query/blockAgentOnConnection.js", () => ({
  useBlockAgentOnConnectionMutation: () => ({
    mutate: mocks.blockOnConnection,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/removeAgentConnectionBlock.js", () => ({
  useRemoveAgentConnectionBlockMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
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

function setup(subject = agent) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <TooltipProvider>
          <AgentCredentialAccess agent={subject} />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.blocks = [];
  mocks.sessions = [];
  mocks.clientPages = [];
  mocks.canReadOrg = false;
  mocks.listSessions.mockImplementation(() => pages([mocks.sessions]));
  mocks.list.mockResolvedValue([
    {
      id: "grant_connect",
      scope: "mcp:connect",
      effect: "allow",
      selector: { resourceKind: "mcp", resourceId: "*" },
    },
    {
      id: "grant_block",
      scope: "mcp:blocked_auth",
      effect: "allow",
      selector: {
        resourceKind: "mcp_auth_client",
        resourceId: "*",
        clientType: "remote_session",
      },
    },
  ]);
  mocks.create.mockResolvedValue({});
});

const allCredentials = {
  id: "grant_all",
  scope: "mcp:auth",
  effect: "allow",
  selector: { resourceKind: "mcp_auth_client", resourceId: "*" },
};
const reportingBot = {
  id: "client_bot",
  name: "Reporting bot",
  clientType: "service_account",
  enabled: true,
  servers: [],
};

/** The use cell of one credentials row, named by what the row covers. */
function cell(label: string) {
  return screen.findByRole("button", {
    name: (name) => name === `Use ${label}` || name === `Use ${label}, blocked`,
  });
}

describe("AgentCredentialAccess", () => {
  it("shows a block on a kind of credential as blocked", async () => {
    setup();
    expect(
      (await cell("Each person's own account")).getAttribute("aria-label"),
    ).toBe("Use Each person's own account, blocked");
  });

  it("lifts a block through the agent's policy on save", async () => {
    setup();
    fireEvent.click(await cell("Each person's own account"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        deleteAgentPolicyGrantForm: {
          agentId: "agent_example",
          grantId: "grant_block",
        },
      }),
    );
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("takes one credential away from all of them without a gap", async () => {
    mocks.canReadOrg = true;
    mocks.clientPages = [[reportingBot]];
    mocks.list.mockResolvedValue([allCredentials]);
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: /^Service account/ }),
    );
    fireEvent.click(await cell("Reporting bot"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.remove).toHaveBeenCalledOnce());
    expect(mocks.remove).toHaveBeenCalledWith({
      deleteAgentPolicyGrantForm: {
        agentId: "agent_example",
        grantId: "grant_all",
      },
    });
    // Every other kind is still allowed, and was added before the wide rule
    // went.
    const created = mocks.create.mock.calls.map(
      ([form]) => form.createAgentPolicyGrantForm.selector.clientType,
    );
    expect(created).toEqual([
      "remote_session",
      "static_credential",
      "pass_through",
      "ema",
    ]);
    expect(mocks.create.mock.invocationCallOrder.at(-1)!).toBeLessThan(
      mocks.remove.mock.invocationCallOrder[0]!,
    );
  });

  it("leaves a connection owner's block out of what a manager edits", async () => {
    mocks.list.mockResolvedValue([
      {
        id: "owner_block",
        scope: "mcp:blocked_auth",
        effect: "allow",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          connectionOwner: "user_other",
          upstreamClient: "registration_example",
        },
      },
    ]);
    setup();
    expect(
      await screen.findByText(/1 person has also blocked this agent/),
    ).toBeTruthy();
    expect(screen.queryByText("Other rules")).toBeNull();
  });

  it("lets anyone block the agent on their own connection", async () => {
    mocks.sessions = [
      {
        remoteSessionClientId: "registration_example",
        upstreamDisplayName: "Work account",
      },
    ];
    setup({ ...agent, permissions: { ...agent.permissions, write: false } });
    expect(screen.queryByText("Upstream credentials")).toBeNull();
    fireEvent.click(
      await screen.findByRole("button", { name: "Block on a connection" }),
    );
    expect(mocks.listSessions).toHaveBeenCalledWith(
      { subjectUrn: "user:user_example", limit: 100 },
      undefined,
      { signal: expect.any(AbortSignal) },
    );
    fireEvent.click(
      screen.getByRole("combobox", { name: "Connection to block" }),
    );
    fireEvent.click(screen.getByRole("option", { name: "Work account" }));
    fireEvent.click(screen.getByRole("button", { name: "Block" }));
    expect(mocks.blockOnConnection).toHaveBeenCalledWith({
      request: {
        blockAgentOnConnectionForm: {
          agentId: "agent_example",
          remoteSessionClientId: "registration_example",
        },
      },
    });
  });
});

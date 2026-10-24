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
import { AgentCredentialRestrictions } from "./AgentCredentialRestrictions";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  remove: vi.fn(),
  blockOnConnection: vi.fn(),
  blocks: [] as unknown[],
  sessions: [] as unknown[],
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false, isLoading: false, error: null }),
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
    },
  }),
}));
vi.mock("@gram/client/react-query/authClients.js", () => ({
  useAuthClients: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/agentConnectionBlocks.js", () => ({
  invalidateAllAgentConnectionBlocks: () => Promise.resolve(),
  useAgentConnectionBlocks: () => ({
    data: { result: { items: mocks.blocks } },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/remoteSessions.js", () => ({
  useRemoteSessions: () => ({ data: { result: { items: mocks.sessions } } }),
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
        <AgentCredentialRestrictions agent={subject} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.blocks = [];
  mocks.sessions = [];
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

describe("AgentCredentialRestrictions", () => {
  it("lists the agent's credential blocks in words", async () => {
    setup();
    expect(
      await screen.findByText(
        "Each person's own account · every credential of this kind",
      ),
    ).toBeTruthy();
  });

  it("blocks a kind of credential through the agent's policy", async () => {
    setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Block a credential" }),
    );
    fireEvent.click(
      screen.getByRole("combobox", { name: "Credential to block" }),
    );
    fireEvent.click(screen.getByRole("option", { name: "Service account" }));
    fireEvent.click(screen.getByRole("button", { name: "Block" }));
    await waitFor(() =>
      expect(mocks.create).toHaveBeenCalledWith({
        createAgentPolicyGrantForm: {
          agentId: "agent_example",
          effect: "allow",
          scope: "mcp:blocked_auth",
          selector: {
            resourceKind: "mcp_auth_client",
            resourceId: "*",
            clientType: "service_account",
          },
        },
      }),
    );
  });

  it("lets anyone block the agent on their own connection", () => {
    mocks.sessions = [
      {
        remoteSessionClientId: "registration_example",
        upstreamDisplayName: "Work account",
      },
    ];
    setup({ ...agent, permissions: { ...agent.permissions, write: false } });
    expect(
      screen.queryByRole("button", { name: "Block a credential" }),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Block on a connection" }),
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

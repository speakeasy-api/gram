import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  AgentIdentityAudit,
  AgentIdentityChallenges,
} from "./AgentIdentityActivity";

const mocks = vi.hoisted(() => ({
  audit: vi.fn(),
  challenges: vi.fn(),
  maps: vi.fn(),
}));
const from = new Date("2026-01-01T00:00:00Z");
const to = new Date("2026-01-08T00:00:00Z");
vi.mock("./useIdentityQueries", () => ({
  useIdentityWindow: () => ({ from, to }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
// Resolving actor names reads the agent and member inventories, which want a
// query client and a session. These tests are about which audit query the
// panel sends, so the lookup is stood in for rather than provided.
vi.mock("@/components/auditlogs/audit-principals", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useAuditPrincipals: () => ({ agents: new Map(), users: new Map() }),
}));
// Server and project names for grant changes come from the org inventories.
vi.mock("@/pages/access/useResourceLinkMaps", () => ({
  useResourceLinkMaps: (...args: unknown[]) => {
    mocks.maps(...args);
    return {
      projectMap: new Map(),
      toolsetMap: new Map([
        ["server_ops", { slug: "acme-ops", name: "Acme Ops", projectId: "p" }],
      ]),
      mcpServerMap: new Map(),
    };
  },
}));
vi.mock("@gram/client/react-query/auditLogs.js", () => ({
  useAuditLogs: (...args: unknown[]) => mocks.audit(...args),
}));
vi.mock("@gram/client/react-query/challenges.js", () => ({
  useChallenges: (...args: unknown[]) => mocks.challenges(...args),
}));
const agent = {
  id: "agent_example",
  ownerUserId: "owner_example",
} as ManagedAgent;
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.audit.mockReturnValue({ data: { result: { logs: [] } } });
  mocks.challenges.mockReturnValue({ data: { challenges: [], total: 0 } });
});
it("queries changes to the agent as the audit subject, including human permission edits", () => {
  render(<AgentIdentityAudit agent={agent} subject />);
  expect(mocks.audit).toHaveBeenCalledWith(
    { subjectType: "agent", subjectId: agent.id, from, to },
    undefined,
    { throwOnError: false },
  );
});
it("names each change in words rather than its audit action", async () => {
  mocks.audit.mockReturnValue({
    data: {
      result: {
        logs: [
          {
            id: "log_one",
            action: "agent:policy_grant_create",
            actorId: "owner_example",
            actorDisplayName: "Example Owner",
            createdAt: from,
          },
        ],
      },
    },
  });
  render(<AgentIdentityAudit agent={agent} subject />);
  const label = screen.getByText("Added direct policy grant to agent");
  expect(screen.queryByText("agent:policy_grant_create")).toBeNull();

  // The code is still there for whoever needs to match it to a log.
  fireEvent.focus(label);
  expect(
    (await screen.findAllByText("agent:policy_grant_create")).length,
  ).toBeGreaterThan(0);
});
it("says which server a grant change touched and which connection was granted", () => {
  mocks.audit.mockReturnValue({
    data: {
      result: {
        logs: [
          {
            id: "log_server",
            action: "agent:policy_grant_create",
            actorId: "owner_example",
            actorDisplayName: "Example Owner",
            createdAt: from,
            afterSnapshot: {
              scope: "mcp:connect",
              selector: { resource_kind: "mcp", resource_id: "server_ops" },
            },
          },
          {
            id: "log_connection",
            action: "agent:connection_attach",
            actorId: "owner_example",
            actorDisplayName: "Example Owner",
            createdAt: from,
            metadata: { upstream_account: "owner@example.com" },
          },
        ],
      },
    },
  });
  render(<AgentIdentityAudit agent={agent} subject />);
  expect(screen.getByText(/mcp:connect · Acme Ops/)).toBeTruthy();
  expect(screen.getByText("Granted their connection to agent")).toBeTruthy();
  expect(screen.getByText(/Signs in as owner@example.com/)).toBeTruthy();
});
it("loads server names for a connection block so it names the server", () => {
  mocks.audit.mockReturnValue({
    data: {
      result: {
        logs: [
          {
            id: "log_block",
            action: "agent:connection_block_create",
            actorId: "owner_example",
            actorDisplayName: "Example Owner",
            createdAt: from,
            afterSnapshot: {
              scope: "mcp:connect",
              selector: { resource_kind: "mcp", resource_id: "server_ops" },
            },
          },
        ],
      },
    },
  });
  render(<AgentIdentityAudit agent={agent} subject />);
  expect(mocks.maps).toHaveBeenLastCalledWith(true);
  expect(screen.getByText(/mcp:connect · Acme Ops/)).toBeTruthy();
});
it("skips the server and project lists when no listed change names one", () => {
  mocks.audit.mockReturnValue({
    data: {
      result: {
        logs: [
          {
            id: "log_rename",
            action: "agent:rename",
            actorId: "owner_example",
            actorDisplayName: "Example Owner",
            createdAt: from,
          },
        ],
      },
    },
  });
  render(<AgentIdentityAudit agent={agent} subject />);
  expect(mocks.maps).toHaveBeenLastCalledWith(false);
});
it("shows a change with no words of its own as its audit code", () => {
  mocks.audit.mockReturnValue({
    data: {
      result: {
        logs: [
          {
            id: "log_two",
            action: "agent:something_new",
            actorId: "owner_example",
            actorDisplayName: "Example Owner",
            createdAt: from,
          },
        ],
      },
    },
  });
  render(<AgentIdentityAudit agent={agent} subject />);
  expect(screen.getByText("agent:something_new")).toBeTruthy();
});
it("queries agent actions without using its owner's identity", () => {
  render(<AgentIdentityAudit agent={agent} />);
  expect(mocks.audit).toHaveBeenCalledWith(
    { actorId: agent.id, from, to },
    undefined,
    { throwOnError: false },
  );
});
it("queries authorization checks using the agent principal and preserves it in the handoff", () => {
  render(
    <MemoryRouter>
      <AgentIdentityChallenges agent={agent} />
    </MemoryRouter>,
  );
  expect(mocks.challenges).toHaveBeenCalledWith(
    { principalUrn: "agent:agent_example", from, to, limit: 25 },
    undefined,
    { throwOnError: false },
  );
  expect(screen.getByRole("link").getAttribute("href")).toContain(
    "identity=agent%3Aagent_example",
  );
});
it("shows failed authorization reads as an error rather than no checks", () => {
  mocks.challenges.mockReturnValue({ isError: true, refetch: vi.fn() });
  render(
    <MemoryRouter>
      <AgentIdentityChallenges agent={agent} />
    </MemoryRouter>,
  );
  expect(screen.getByText("This panel could not be loaded.")).toBeTruthy();
  expect(screen.queryByText(/No authorization checks recorded/)).toBeNull();
});

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
      projectMap: new Map([
        ["project_one", { slug: "project-one", name: "Project one" }],
      ]),
      toolsetMap: new Map([
        ["server_ops", { slug: "acme-ops", name: "Acme Ops", projectId: "p" }],
      ]),
      mcpServerMap: new Map([
        ["server_slug_only", { slug: "slug-only-server", projectId: "p" }],
      ]),
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
function renderChecks() {
  render(
    <MemoryRouter>
      <AgentIdentityChallenges agent={agent} />
    </MemoryRouter>,
  );
}
const at = (minute: number) =>
  new Date(`2026-01-07T10:${String(minute).padStart(2, "0")}:00Z`);
const row = (
  outcome: string,
  scope: string,
  resourceId: string,
  minute: number,
  extra: Record<string, unknown> = {},
) => ({
  id: `${outcome}-${scope}-${resourceId}-${minute}`,
  scope,
  resourceKind: "mcp",
  resourceId,
  operation: "require",
  outcome,
  timestamp: at(minute),
  ...extra,
});
it("reads failed checks, server connections and other checks apart, leaving out only allowed hook ingestion, and keeps the agent in the handoff", () => {
  renderChecks();
  // Denied hook events are real failures; only allowed ones are routine.
  const slices = [
    { outcome: "deny", excludeScopes: [] },
    { outcome: "allow", scope: "mcp:connect", excludeScopes: [] },
    { outcome: "allow", excludeScopes: ["org:hooks_ingest", "mcp:connect"] },
  ];
  expect(mocks.challenges).toHaveBeenCalledTimes(slices.length);
  for (const slice of slices) {
    expect(mocks.challenges).toHaveBeenCalledWith(
      {
        principalUrn: "agent:agent_example",
        ...slice,
        from,
        to,
        limit: expect.any(Number),
      },
      undefined,
      { throwOnError: false },
    );
  }
  expect(screen.getByRole("link").getAttribute("href")).toContain(
    "identity=agent%3Aagent_example",
  );
});
it("lists failed checks first, then the servers the agent reached by name, then other checks", () => {
  mocks.challenges.mockImplementation(
    ({ outcome, scope }: { outcome: string; scope?: string }) => {
      if (outcome === "deny") {
        return {
          data: {
            challenges: [
              row("deny", "mcp:connect", "server_linear", 9),
              row("deny", "mcp:connect", "server_linear", 4),
            ],
            total: 2,
          },
        };
      }
      if (scope === "mcp:connect") {
        return {
          data: {
            challenges: [
              row("allow", "mcp:connect", "server_ops", 8),
              row("allow", "mcp:connect", "server_ops", 7),
              row("allow", "mcp:connect", "server_ops", 6),
              row("allow", "mcp:connect", "server_slug_only", 5),
            ],
            total: 4,
          },
        };
      }
      return {
        data: {
          challenges: [
            row("allow", "project:read", "project_one", 5, {
              resourceKind: "project",
            }),
          ],
          total: 1,
        },
      };
    },
  );
  renderChecks();
  const failedHeading = screen.getByRole("heading", { name: "Failed" });
  const serversHeading = screen.getByRole("heading", { name: "MCP servers" });
  const otherHeading = screen.getByRole("heading", {
    name: "Other allowed checks",
  });
  expect(
    failedHeading.compareDocumentPosition(serversHeading) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(
    serversHeading.compareDocumentPosition(otherHeading) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(screen.getByText(/Denied 2 times/)).toBeTruthy();
  // A server the org lists is named; one it does not keeps its id.
  expect(screen.getByText("Acme Ops")).toBeTruthy();
  expect(screen.getByText(/Allowed 3 times/)).toBeTruthy();
  expect(screen.getByTitle("server_linear")).toBeTruthy();
  // A server listed with no name falls back to its slug.
  expect(screen.getByText("slug-only-server")).toBeTruthy();
  // Other checks name their resource as Roles & Permissions does.
  expect(screen.getByText("Project one")).toBeTruthy();
  expect(screen.getAllByText(/Allowed once/)).toHaveLength(2);
  // Failed connections come with how gateway listings record them.
  expect(
    screen.getByText(/The agent gateway checks every server/),
  ).toBeTruthy();
  expect(
    screen.getByText(/2 failed and 5 allowed checks in this period/),
  ).toBeTruthy();
  expect(screen.queryByText(/Grouped from the latest/)).toBeNull();
});
it("reads a cut-off slice's counts as lower bounds and says resources may be missing", () => {
  mocks.challenges.mockImplementation(({ outcome }: { outcome: string }) =>
    outcome === "deny"
      ? {
          data: {
            challenges: [
              row("deny", "mcp:read", "server_linear", 9),
              row("deny", "mcp:read", "server_linear", 4),
            ],
            total: 202,
          },
        }
      : { data: { challenges: [], total: 0 } },
  );
  renderChecks();
  expect(screen.getByText(/Denied at least 2 times/)).toBeTruthy();
  expect(
    screen.getByText(/Grouped from the latest 2 of 202 checks/),
  ).toBeTruthy();
  // No connection was denied, so the gateway note does not apply.
  expect(
    screen.queryByText(/The agent gateway checks every server/),
  ).toBeNull();
});
it("names a tool list filtering pass rather than an empty scope", () => {
  mocks.challenges.mockImplementation(({ outcome }: { outcome: string }) =>
    outcome === "allow"
      ? {
          data: {
            challenges: [
              row("allow", "", "", 3, {
                resourceKind: undefined,
                resourceId: undefined,
                operation: "filter",
              }),
            ],
            total: 1,
          },
        }
      : { data: { challenges: [], total: 0 } },
  );
  renderChecks();
  expect(screen.getAllByText("Tool list filtering").length).toBeGreaterThan(0);
});
it("says when the agent had no failed checks in the period", () => {
  renderChecks();
  expect(
    screen.getByText("No failed checks for this agent in this period."),
  ).toBeTruthy();
});
it("says a slice could not be loaded, with a retry, and leaves it out of the counts", () => {
  const refetch = vi.fn();
  mocks.challenges.mockImplementation(({ outcome }: { outcome: string }) =>
    outcome === "deny"
      ? { isError: true, refetch }
      : {
          data: {
            challenges: [row("allow", "mcp:connect", "server_ops", 3)],
            total: 1,
          },
          refetch: vi.fn(),
        },
  );
  renderChecks();
  expect(screen.getByText(/Failed checks could not be loaded\./)).toBeTruthy();
  expect(
    screen.queryByText("No failed checks for this agent in this period."),
  ).toBeNull();
  // It never had a result, so there is no earlier one to call stale.
  expect(screen.queryByText(/refresh failed/)).toBeNull();
  expect(
    screen.getByText(
      "2 allowed checks in this period. The failed count could not be loaded.",
    ),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(refetch).toHaveBeenCalledOnce();
});
it("marks a slice that failed to refresh as showing its last result", () => {
  mocks.challenges.mockImplementation(({ outcome }: { outcome: string }) => ({
    data: { challenges: [], total: 0 },
    isError: outcome === "deny",
    refetch: vi.fn(),
  }));
  renderChecks();
  expect(screen.getByText(/refresh failed/)).toBeTruthy();
});
it("shows failed authorization reads as an error rather than no checks", () => {
  mocks.challenges.mockReturnValue({ isError: true, refetch: vi.fn() });
  renderChecks();
  expect(screen.getByText("This panel could not be loaded.")).toBeTruthy();
  expect(screen.queryByText(/No authorization checks recorded/)).toBeNull();
});

// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { ToolUsageTargetSummary } from "@gram/client/models/components/toolusagetargetsummary.js";
import type { InventoryServer } from "@/pages/agents/credentials/useServerInventory";
import { summarizeAgentServerUse } from "./agentServerUse";

const PROJECT = "project-1";

function connect(resourceId: string, id = resourceId): AgentPolicyGrant {
  return {
    id: `grant-${id}`,
    scope: "mcp:connect",
    effect: "allow",
    selector: { resourceKind: "mcp", resourceId, projectId: PROJECT },
  } as AgentPolicyGrant;
}

function server(fields: Partial<InventoryServer>): InventoryServer {
  return {
    id: "server",
    resourceId: "server",
    name: "Server",
    slug: "server",
    projectId: PROJECT,
    projectName: "Default",
    projectSlug: "default",
    kind: "Hosted",
    ...fields,
  };
}

function target(
  fields: Partial<ToolUsageTargetSummary>,
): ToolUsageTargetSummary {
  return {
    eventCount: 1,
    failureCount: 0,
    failureRate: 0,
    successCount: 1,
    targetId: "target",
    targetKind: "server",
    targetLabel: "Target",
    targetType: "hosted_mcp_server",
    uniqueTools: 1,
    ...fields,
  } as ToolUsageTargetSummary;
}

const SERVERS = [
  // A toolset served by two MCP servers: a grant on the toolset reaches both.
  server({
    id: "support-a",
    resourceId: "toolset-support",
    name: "Support Tools",
    activityTarget: "hosted_mcp_server:support-tools",
  }),
  server({ id: "support-b", resourceId: "toolset-support", name: "Support" }),
  server({
    id: "tracker",
    resourceId: "tracker",
    name: "Tracker",
    issuerId: "issuer-1",
  }),
  server({ id: "ops", resourceId: "ops", name: "Ops" }),
];

describe("summarizeAgentServerUse", () => {
  it("lists every server the agent can connect to, busiest first, with its calls", () => {
    const { rows, anyServer } = summarizeAgentServerUse({
      grants: [connect("toolset-support"), connect("tracker"), connect("ops")],
      servers: SERVERS,
      targets: [
        target({ targetId: "tracker", mcpServerId: "tracker", eventCount: 3 }),
        // Matched by the server id the call recorded.
        target({
          targetId: "support-b",
          mcpServerId: "support-b",
          eventCount: 4,
        }),
        // Matched by the activity target, with no server id recorded.
        target({ targetId: "support-tools", eventCount: 2 }),
      ],
      projectId: PROJECT,
    });
    expect(anyServer).toBe(false);
    expect(rows).toEqual([
      {
        key: "grant:toolset-support",
        resourceId: "toolset-support",
        name: "Support Tools",
        calls: 6,
      },
      {
        key: "grant:tracker",
        resourceId: "tracker",
        name: "Tracker",
        calls: 3,
      },
      // Reachable but not called: listed with no calls.
      { key: "grant:ops", resourceId: "ops", name: "Ops", calls: 0 },
    ]);
  });

  it("lists a called server the agent has no grant for, and skips gateway, shadow and local calls", () => {
    const { rows } = summarizeAgentServerUse({
      grants: [connect("ops")],
      servers: SERVERS,
      targets: [
        target({
          targetId: "old",
          targetLabel: "Old server",
          mcpServerId: "old",
          eventCount: 5,
        }),
        target({
          targetType: "meta_mcp_server",
          targetId: "gateway",
          eventCount: 9,
        }),
        target({
          targetType: "shadow_mcp_server",
          targetId: "speakeasy",
          eventCount: 9,
        }),
        target({
          targetType: "local_tool",
          targetKind: "local_tools",
          targetId: "bash",
          eventCount: 9,
        }),
      ],
      projectId: PROJECT,
    });
    expect(rows).toEqual([
      {
        key: "target:hosted_mcp_server:old",
        resourceId: "old",
        name: "Old server",
        calls: 5,
      },
      { key: "grant:ops", resourceId: "ops", name: "Ops", calls: 0 },
    ]);
  });

  it("lists the servers it called when a grant reaches every server", () => {
    const { rows, anyServer } = summarizeAgentServerUse({
      grants: [connect("*", "any")],
      servers: SERVERS,
      targets: [
        target({ targetId: "ops", targetLabel: "Ops", mcpServerId: "ops" }),
      ],
      projectId: PROJECT,
    });
    expect(anyServer).toBe(true);
    expect(rows).toEqual([
      {
        key: "target:hosted_mcp_server:ops",
        resourceId: "ops",
        name: "Ops",
        calls: 1,
      },
    ]);
  });

  it("leaves calls uncounted when the usage read is missing or the server is in another project", () => {
    const { rows } = summarizeAgentServerUse({
      grants: [connect("ops"), connect("tracker")],
      servers: [
        ...SERVERS.filter((candidate) => candidate.id !== "tracker"),
        server({
          id: "tracker",
          resourceId: "tracker",
          name: "Tracker",
          projectId: "project-2",
        }),
      ],
      targets: undefined,
      projectId: PROJECT,
    });
    expect(rows.map((row) => row.calls)).toEqual([undefined, undefined]);

    const other = summarizeAgentServerUse({
      grants: [connect("tracker")],
      servers: [
        server({
          id: "tracker",
          resourceId: "tracker",
          name: "Tracker",
          projectId: "project-2",
        }),
      ],
      targets: [],
      projectId: PROJECT,
    });
    expect(other.rows[0]?.calls).toBeUndefined();
  });

  it("ignores other scopes, deny grants and repeated grants", () => {
    const { rows } = summarizeAgentServerUse({
      grants: [
        connect("ops"),
        connect("ops", "ops-again"),
        { ...connect("tracker"), effect: "deny" } as AgentPolicyGrant,
        {
          ...connect("toolset-support"),
          scope: "mcp:read",
        } as AgentPolicyGrant,
      ],
      servers: SERVERS,
      targets: [],
      projectId: PROJECT,
    });
    expect(rows.map((row) => row.key)).toEqual(["grant:ops"]);
  });

  it("names a granted server it cannot list after its calls, else plainly", () => {
    const { rows } = summarizeAgentServerUse({
      grants: [connect("gone"), connect("hidden")],
      servers: [],
      targets: [
        target({
          targetId: "hidden",
          targetLabel: "Hidden",
          mcpServerId: "hidden",
        }),
      ],
      projectId: PROJECT,
    });
    expect(rows.map((row) => row.name)).toEqual([
      "Hidden",
      "Unlisted MCP server",
    ]);
  });
});

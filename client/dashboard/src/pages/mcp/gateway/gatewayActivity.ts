import type { MetaMcpDiscoveryFunnel } from "@gram/client/models/components/metamcpdiscoveryfunnel.js";
import type { MetaMcpMemberUsage } from "@gram/client/models/components/metamcpmemberusage.js";
import type { MemberRow } from "./memberRows";

export interface MetaToolUsageItem {
  key: string;
  label: string;
  value: number;
}

// The gateway's four tools in the order an agent reaches for them: inventory,
// one server's tool list, schemas, then a call.
export function metaToolUsageItems(
  funnel: MetaMcpDiscoveryFunnel,
): MetaToolUsageItem[] {
  return [
    { key: "list_servers", label: "list_servers", value: funnel.listServers },
    {
      key: "describe_server",
      label: "describe_server",
      value: funnel.describeServer,
    },
    {
      key: "describe_tools",
      label: "describe_tools",
      value: funnel.describeTools,
    },
    { key: "execute_tool", label: "execute_tool", value: funnel.executeTool },
  ];
}

export interface MemberUsageRow {
  mcpServerId: string;
  label: string;
  toolCalls: number;
  errorCount: number;
  errorRate: number;
  lastCalledAt: Date | undefined;
}

// Usage rows keyed by the member's mcp_servers id, labeled from the gateway's
// member list. A member that left the gateway keeps its id as the label so
// historical calls stay attributed.
export function memberUsageRows(
  usage: MetaMcpMemberUsage[],
  members: MemberRow[],
): MemberUsageRow[] {
  const labels = new Map<string, string>();
  for (const row of members) {
    labels.set(
      row.member.mcpServerId,
      row.server?.name ||
        row.member.mcpServerName ||
        row.server?.slug ||
        row.member.mcpServerSlug ||
        row.member.mcpServerId,
    );
  }
  return usage.map((entry) => ({
    mcpServerId: entry.mcpServerId,
    label: labels.get(entry.mcpServerId) ?? entry.mcpServerId,
    toolCalls: entry.toolCalls,
    errorCount: entry.errorCount,
    errorRate:
      entry.toolCalls > 0 ? (entry.errorCount / entry.toolCalls) * 100 : 0,
    lastCalledAt: entry.lastCalledAt,
  }));
}

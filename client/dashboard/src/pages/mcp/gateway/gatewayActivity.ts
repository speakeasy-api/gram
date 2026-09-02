import type { MetaMcpDiscoveryFunnel } from "@gram/client/models/components/metamcpdiscoveryfunnel.js";
import type { MetaMcpMemberUsage } from "@gram/client/models/components/metamcpmemberusage.js";
import type { MemberRow } from "./memberRows";

export interface FunnelItem {
  key: string;
  label: string;
  value: number;
}

// Discovery order as an agent walks it: inventory, one server, its schemas,
// then a call. Values are absolute counts so a bar shows drop-off per step.
export function funnelItems(funnel: MetaMcpDiscoveryFunnel): FunnelItem[] {
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

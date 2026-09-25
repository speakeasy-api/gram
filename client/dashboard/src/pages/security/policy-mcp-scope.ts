import type {
  RiskMCPScope,
  RiskMCPScopeToolAnnotations,
} from "@gram/client/models/components/riskmcpscope.js";
import type { RiskMCPServerScope } from "@gram/client/models/components/riskmcpserverscope.js";

type PolicyScopeMode = "everywhere" | "mcp";
export type ToolAnnotation = RiskMCPScopeToolAnnotations;

export interface PolicyMCPScopeValue {
  mode: PolicyScopeMode;
  allServers: boolean;
  toolAnnotations: ToolAnnotation[];
  servers: RiskMCPServerScope[];
}

export function policyMCPScopeValue(
  scope: RiskMCPScope | null | undefined,
): PolicyMCPScopeValue {
  return {
    mode: scope ? "mcp" : "everywhere",
    allServers: scope?.allServers ?? false,
    toolAnnotations: [...(scope?.toolAnnotations ?? [])],
    servers:
      scope?.servers.map((server) => ({
        mcpServerId: server.mcpServerId,
        ...(server.tools === undefined ? {} : { tools: [...server.tools] }),
      })) ?? [],
  };
}

export function policyMCPScopePayload(
  value: PolicyMCPScopeValue,
): RiskMCPScope | null {
  if (value.mode === "everywhere") return null;
  return {
    allServers: value.allServers,
    toolAnnotations: [...value.toolAnnotations].sort(),
    servers: value.servers
      .map((server) => ({
        mcpServerId: server.mcpServerId,
        ...(server.tools === undefined
          ? {}
          : { tools: [...server.tools].sort() }),
      }))
      .sort((left, right) => left.mcpServerId.localeCompare(right.mcpServerId)),
  };
}

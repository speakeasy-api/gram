import type {
  RiskMCPScope,
  RiskMCPScopeToolAnnotations,
} from "@gram/client/models/components/riskmcpscope.js";
import type { RiskMCPServerScope } from "@gram/client/models/components/riskmcpserverscope.js";

// "unset" exists only in a fresh create form: the author picks a scope before
// the policy can be saved. Stored policies always hydrate to a real mode.
export type PolicyScopeMode = "everywhere" | "mcp" | "unset";
export type PolicyScopeChoice = Exclude<PolicyScopeMode, "unset">;
export type ToolAnnotation = RiskMCPScopeToolAnnotations;

// Mirrors AllToolsWildcard in server/internal/risk/policycore/types.go.
export const ALL_TOOLS_WILDCARD = "*";

export const POLICY_SCOPE_MODE_LABEL: Record<PolicyScopeChoice, string> = {
  everywhere: "Client sessions",
  mcp: "Specific MCP servers",
};

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

export function initialPolicyMCPScopeValue(
  policy: { mcpScope?: RiskMCPScope | null } | null | undefined,
): PolicyMCPScopeValue {
  if (policy) return policyMCPScopeValue(policy.mcpScope);
  return { mode: "unset", allServers: false, toolAnnotations: [], servers: [] };
}

export function policyMCPScopePayload(
  value: PolicyMCPScopeValue,
): RiskMCPScope | null {
  if (value.mode !== "mcp") return null;
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

// True once the scope is something the backend can act on.
export function policyMCPScopeComplete(value: PolicyMCPScopeValue): boolean {
  switch (value.mode) {
    case "everywhere":
      return true;
    case "mcp":
      return value.allServers || value.servers.length > 0;
    case "unset":
      return false;
  }
}

export function policyScopeSummary(value: PolicyMCPScopeValue): string {
  switch (value.mode) {
    case "everywhere":
      return POLICY_SCOPE_MODE_LABEL.everywhere;
    case "mcp": {
      const count = value.servers.length;
      const servers = value.allServers
        ? "all servers"
        : `${count} server${count === 1 ? "" : "s"}`;
      return `${POLICY_SCOPE_MODE_LABEL.mcp} · ${servers}`;
    }
    case "unset":
      return "Not chosen";
  }
}

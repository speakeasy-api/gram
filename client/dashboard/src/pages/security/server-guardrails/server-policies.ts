import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { ALL_TOOLS_WILDCARD } from "../policy-mcp-scope";

export interface ServerPolicies {
  /** Policies whose scope names this server. */
  scoped: RiskPolicy[];
  /** Policies that reach this server without naming it: org-wide policies and
   *  ones scoped to every MCP server. */
  inherited: RiskPolicy[];
}

function scopeEntry(policy: RiskPolicy, mcpServerId: string) {
  return policy.mcpScope?.servers.find(
    (server) => server.mcpServerId === mcpServerId,
  );
}

/** Splits a project's policies into those that name `mcpServerId` and those
 *  that apply to it anyway. A per-server entry under an all-servers scope is a
 *  tool override, so it counts as scoped. */
export function policiesForMcpServer(
  policies: RiskPolicy[],
  mcpServerId: string,
): ServerPolicies {
  const scoped: RiskPolicy[] = [];
  const inherited: RiskPolicy[] = [];
  for (const policy of policies) {
    if (scopeEntry(policy, mcpServerId)) {
      scoped.push(policy);
    } else if (!policy.mcpScope || policy.mcpScope.allServers) {
      inherited.push(policy);
    }
  }
  return { scoped, inherited };
}

/** "All tools" or "N tools" for how a scoped policy covers `mcpServerId`. */
export function scopedToolsLabel(
  policy: RiskPolicy,
  mcpServerId: string,
): string {
  const tools = scopeEntry(policy, mcpServerId)?.tools;
  // An empty list matches every tool, like the wildcard.
  if (!tools || tools.length === 0 || tools.includes(ALL_TOOLS_WILDCARD)) {
    return "All tools";
  }
  return tools.length === 1 ? "1 tool" : `${tools.length} tools`;
}

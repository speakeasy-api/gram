import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { ALL_TOOLS_WILDCARD } from "../policy-mcp-scope";

export interface ServerPolicies {
  /** Policies whose scope names this server, or a gateway it belongs to. */
  scoped: RiskPolicy[];
  /** Policies scoped to every MCP server. */
  inherited: RiskPolicy[];
}

function scopeEntry(policy: RiskPolicy, mcpServerId: string) {
  return policy.mcpScope?.servers.find(
    (server) => server.mcpServerId === mcpServerId,
  );
}

/** The policies to list on a server's Guardrails tab: the enabled ones the
 *  server resolves as applying (including gateway membership), plus disabled
 *  ones that name this server or every server, so they can be re-enabled.
 *  Disabled policies scoped only through a gateway are not listed: resolving
 *  gateway membership happens server-side and covers enabled policies only. */
export function serverGuardrailPolicies(
  applicable: RiskPolicy[],
  all: RiskPolicy[],
  mcpServerId: string,
): RiskPolicy[] {
  const seen = new Set(applicable.map((policy) => policy.id));
  const disabled = all.filter(
    (policy) =>
      !policy.enabled &&
      !seen.has(policy.id) &&
      (policy.mcpScope?.allServers === true ||
        scopeEntry(policy, mcpServerId) !== undefined),
  );
  return [...applicable, ...disabled];
}

/** Splits the policies that apply to `mcpServerId` (as resolved by the
 *  server, including gateway membership) into those scoped to it and those
 *  inherited from an all-servers scope. A per-server entry under an
 *  all-servers scope is a tool override, so it counts as scoped. */
export function policiesForMcpServer(
  applicable: RiskPolicy[],
  mcpServerId: string,
): ServerPolicies {
  const scoped: RiskPolicy[] = [];
  const inherited: RiskPolicy[] = [];
  for (const policy of applicable) {
    if (policy.mcpScope?.allServers && !scopeEntry(policy, mcpServerId)) {
      inherited.push(policy);
    } else {
      scoped.push(policy);
    }
  }
  return { scoped, inherited };
}

/** How a scoped policy covers `mcpServerId`: "All tools", "N tools", or
 *  "Through a gateway" when the scope names a gateway the server belongs to. */
export function scopedToolsLabel(
  policy: RiskPolicy,
  mcpServerId: string,
): string {
  const entry = scopeEntry(policy, mcpServerId);
  if (!entry && !policy.mcpScope?.allServers) return "Through a gateway";
  const tools = entry?.tools;
  // An empty list matches every tool, like the wildcard.
  if (!tools || tools.length === 0 || tools.includes(ALL_TOOLS_WILDCARD)) {
    return "All tools";
  }
  return tools.length === 1 ? "1 tool" : `${tools.length} tools`;
}

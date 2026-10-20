import type { RiskResult } from "@gram/client/models/components/riskresult.js";

export type MCPFindingNames = {
  serverNames: ReadonlyMap<string, string>;
  toolsetNames: ReadonlyMap<string, string>;
};

type MCPServerNameSource = {
  id: string;
  name?: string | undefined;
  slug?: string | undefined;
};

type ToolsetNameSource = {
  id: string;
  name: string;
};

export function buildMCPFindingNames(
  servers: MCPServerNameSource[],
  toolsets: ToolsetNameSource[],
): MCPFindingNames {
  const serverNames = new Map<string, string>();
  for (const server of servers) {
    serverNames.set(
      server.id,
      server.name?.trim() || server.slug || "MCP server",
    );
  }

  const toolsetNames = new Map<string, string>();
  for (const toolset of toolsets) {
    toolsetNames.set(toolset.id, toolset.name);
  }

  return { serverNames, toolsetNames };
}

export function isMCPFinding(finding: RiskResult): boolean {
  return Boolean(
    finding.mcpServerId ||
    finding.metaMcpServerId ||
    finding.toolsetId ||
    finding.mediationSurface ||
    finding.mcpMethod,
  );
}

export function mediationSurfaceLabel(
  surface: string | undefined,
): string | undefined {
  if (surface === "hosted_mcp") return "Hosted MCP";
  if (surface === "remote_mcp") return "Remote MCP";
  if (surface === "shadow_mcp") return "Shadow MCP";
  if (!surface) return undefined;
  return surface
    .split("_")
    .map((word) =>
      word.toLowerCase() === "mcp"
        ? "MCP"
        : word.charAt(0).toUpperCase() + word.slice(1),
    )
    .join(" ");
}

const EMPTY_MCP_FINDING_NAMES: MCPFindingNames = {
  serverNames: new Map(),
  toolsetNames: new Map(),
};

/** Display name of the server, gateway, or toolset an MCP finding came from. */
export function mcpFindingTargetName(
  finding: RiskResult,
  names: MCPFindingNames = EMPTY_MCP_FINDING_NAMES,
): string {
  const resolved =
    (finding.mcpServerId
      ? names.serverNames.get(finding.mcpServerId)
      : undefined) ??
    (finding.metaMcpServerId
      ? names.serverNames.get(finding.metaMcpServerId)
      : undefined) ??
    (finding.toolsetId ? names.toolsetNames.get(finding.toolsetId) : undefined);
  if (resolved) return resolved;
  if (finding.mcpServerId) return "MCP server";
  if (finding.metaMcpServerId) return "MCP gateway";
  return "MCP toolset";
}

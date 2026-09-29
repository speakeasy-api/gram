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

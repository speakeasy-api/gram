import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/utils";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { isMCPFinding, type MCPFindingNames } from "./mcp-finding-context";

const EMPTY_MCP_FINDING_NAMES: MCPFindingNames = {
  serverNames: new Map(),
  toolsetNames: new Map(),
};

function outcomeLabel(outcome: string | undefined): string | undefined {
  if (outcome === "denied" || outcome === "withheld") return "Blocked";
  if (outcome === "logged") return "Logged";
  if (!outcome) return undefined;
  return outcome
    .split("_")
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

function mediationSurfaceLabel(
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

export function MCPFindingContext({
  finding,
  names,
  className,
}: {
  finding: RiskResult;
  names?: MCPFindingNames;
  className?: string;
}): JSX.Element | null {
  if (!isMCPFinding(finding)) return null;
  const resolvedNames = names ?? EMPTY_MCP_FINDING_NAMES;

  const target =
    (finding.mcpServerId
      ? resolvedNames.serverNames.get(finding.mcpServerId)
      : undefined) ??
    (finding.metaMcpServerId
      ? resolvedNames.serverNames.get(finding.metaMcpServerId)
      : undefined) ??
    (finding.toolsetId
      ? resolvedNames.toolsetNames.get(finding.toolsetId)
      : undefined) ??
    (finding.mcpServerId
      ? "MCP server"
      : finding.metaMcpServerId
        ? "MCP gateway"
        : "MCP toolset");
  const detail = [
    mediationSurfaceLabel(finding.mediationSurface),
    finding.toolName,
    outcomeLabel(finding.enforcementOutcome),
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <div className="flex min-w-0 items-center gap-1.5">
        <Badge variant="information" size="sm">
          MCP
        </Badge>
        <span className="min-w-0 truncate" title={target}>
          {target}
        </span>
      </div>
      {detail && (
        <span
          className="text-muted-foreground truncate font-mono text-xs"
          title={detail}
        >
          {detail}
        </span>
      )}
    </div>
  );
}

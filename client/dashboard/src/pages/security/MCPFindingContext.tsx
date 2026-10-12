import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/utils";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import {
  isMCPFinding,
  mcpFindingTargetName,
  mediationSurfaceLabel,
  type MCPFindingNames,
} from "./mcp-finding-context";

function outcomeLabel(outcome: string | undefined): string | undefined {
  if (outcome === "denied" || outcome === "withheld") return "Blocked";
  if (outcome === "logged") return "Logged";
  if (!outcome) return undefined;
  return outcome
    .split("_")
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

// Two lines, tool first: the server on top, then "{tool} · {surface}" since the
// tool is the more specific of the two.
export function MCPFindingContext({
  finding,
  names,
  className,
  showOutcome = false,
}: {
  finding: RiskResult;
  names?: MCPFindingNames;
  className?: string;
  /** Append the enforcement outcome, for surfaces with no outcome column. */
  showOutcome?: boolean;
}): JSX.Element | null {
  if (!isMCPFinding(finding)) return null;

  const target = mcpFindingTargetName(finding, names);
  const detail = [
    finding.toolName,
    mediationSurfaceLabel(finding.mediationSurface),
    showOutcome ? outcomeLabel(finding.enforcementOutcome) : undefined,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <div className="flex min-w-0 items-center gap-1.5">
        <Badge variant="information" size="sm">
          MCP
        </Badge>
        <span className="text-foreground min-w-0 truncate" title={target}>
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

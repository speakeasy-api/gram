import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { CircleDashed, TriangleAlert } from "lucide-react";
import type { McpActivityStatus } from "./mcp-activity";

interface MCPActivityIndicatorProps {
  status: McpActivityStatus;
  // Length of the recent-activity window, echoed by the backend, used to phrase
  // the stale tooltip precisely (defaults to two weeks).
  recentWindowDays?: number;
  size?: "sm" | "md";
  className?: string;
}

function windowLabel(days: number): string {
  if (days === 14) return "2 weeks";
  if (days === 7) return "a week";
  if (days % 7 === 0) return `${days / 7} weeks`;
  return `${days} days`;
}

/**
 * A subtle, icon-only marker for MCP servers with no recent tool-call activity.
 * `never` (muted, dashed circle) reads as informational; `stale` (warning
 * triangle) reads as an alert. Healthy servers render no indicator at all, so
 * this only ever appears when something is worth noticing.
 */
export function MCPActivityIndicator({
  status,
  recentWindowDays = 14,
  size = "md",
  className,
}: MCPActivityIndicatorProps): JSX.Element {
  const iconSize = size === "sm" ? "h-3.5 w-3.5" : "h-4 w-4";

  if (status === "never") {
    return (
      <SimpleTooltip tooltip="This MCP server has never received a tool call.">
        <span
          className={cn(
            "text-muted-foreground/70 inline-flex items-center",
            className,
          )}
          aria-label="No tool calls yet"
        >
          <CircleDashed className={iconSize} />
        </span>
      </SimpleTooltip>
    );
  }

  return (
    <SimpleTooltip
      tooltip={`No tool calls in the last ${windowLabel(recentWindowDays)}.`}
    >
      <span
        className={cn("text-warning inline-flex items-center", className)}
        aria-label={`No tool calls in the last ${windowLabel(recentWindowDays)}`}
      >
        <TriangleAlert className={iconSize} />
      </span>
    </SimpleTooltip>
  );
}

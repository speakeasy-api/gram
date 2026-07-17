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
  if (days === 1) return "1 day";
  if (days % 7 === 0) return `${days / 7} weeks`;
  return `${days} days`;
}

/**
 * A subtle, icon-only marker for MCP servers with no recent tool-call activity.
 * `never` (muted, dashed circle) reads as informational; `stale` (warning
 * triangle) reads as an alert. Healthy servers render no indicator at all, so
 * this only ever appears when something is worth noticing.
 *
 * The trigger is a focusable, labelled element (`role="img"`, `tabIndex=0`) so
 * keyboard and screen-reader users can reach it and hear the same status that
 * pointer users get from the hover tooltip.
 */
export function MCPActivityIndicator({
  status,
  recentWindowDays = 14,
  size = "md",
  className,
}: MCPActivityIndicatorProps): JSX.Element {
  const iconSize = size === "sm" ? "h-3.5 w-3.5" : "h-4 w-4";

  const config =
    status === "never"
      ? {
          tooltip: "This MCP server has never received a tool call.",
          label: "No tool calls yet",
          color: "text-muted-foreground/70",
          Icon: CircleDashed,
        }
      : {
          tooltip: `No tool calls in the last ${windowLabel(recentWindowDays)}.`,
          label: `No tool calls in the last ${windowLabel(recentWindowDays)}`,
          color: "text-warning",
          Icon: TriangleAlert,
        };

  const { Icon } = config;

  return (
    <SimpleTooltip tooltip={config.tooltip}>
      <span
        role="img"
        aria-label={config.label}
        tabIndex={0}
        className={cn(
          "focus-visible:ring-ring inline-flex items-center rounded-sm focus-visible:ring-2 focus-visible:outline-none",
          config.color,
          className,
        )}
      >
        <Icon className={iconSize} />
      </span>
    </SimpleTooltip>
  );
}

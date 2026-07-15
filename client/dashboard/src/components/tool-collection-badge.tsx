import { ReactElement } from "react";
import { cn } from "@/lib/utils";
import { Stack } from "@/components/ui/stack";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { TriangleAlert } from "lucide-react";

export const ToolCollectionBadge = ({
  toolNames,
  count,
  variant = "neutral",
  className,
  warnOnTooManyTools = false,
  emptyLabel,
}: {
  toolNames?: string[] | undefined;
  /**
   * Tool count to display when the per-tool names aren't available (e.g.
   * catalog list entries omit tool definitions to keep the payload small).
   * Ignored when `toolNames` is provided; renders a count badge with no
   * name tooltip.
   */
  count?: number;
  variant?: React.ComponentProps<typeof Badge>["variant"];
  className?: string;
  warnOnTooManyTools?: boolean;
  /**
   * What to render when there are no tools.
   * - undefined (default): the "No Tools" badge.
   * - null: render nothing.
   * - ReactNode: render this instead.
   */
  emptyLabel?: React.ReactNode | null;
}): ReactElement | null => {
  const toolCount = toolNames ? toolNames.length : (count ?? 0);

  let tooltipContent: React.ReactNode = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {toolNames?.map((tool, i) => (
          <p key={i}>{tool}</p>
        ))}
      </Stack>
    </div>
  );

  const toolsWarnings = warnOnTooManyTools && toolCount > 40;
  if (toolsWarnings) {
    tooltipContent = (
      <>
        LLM tool-use performance typically degrades with MCP server size.
        General industry standards recommend keeping MCP servers at around 40
        tools or fewer
      </>
    );
  }

  if (toolCount === 0) {
    if (emptyLabel === null) return null;
    if (emptyLabel !== undefined) return <>{emptyLabel}</>;
    return (
      <Badge variant={variant} className={className}>
        No Tools
      </Badge>
    );
  }

  const badge = (
    <Badge
      variant={toolsWarnings ? "warning" : variant}
      className={cn(!toolsWarnings && "bg-card", className)}
    >
      {toolsWarnings && (
        <Badge.LeftIcon>
          <TriangleAlert className="inline-block" />
        </Badge.LeftIcon>
      )}
      <Badge.Text>
        {toolCount} Tool
        {toolCount === 1 ? "" : "s"}
      </Badge.Text>
    </Badge>
  );

  // Without per-tool names there's nothing to put in a tooltip, so show the
  // bare count badge (unless we're surfacing the too-many-tools warning).
  if (!toolNames && !toolsWarnings) {
    return badge;
  }

  return (
    <Tooltip>
      <TooltipTrigger>{badge}</TooltipTrigger>
      <TooltipContent className="max-w-sm">{tooltipContent}</TooltipContent>
    </Tooltip>
  );
};

export const PoweredBySpeakeasyBadge = (): ReactElement => {
  return (
    <Badge variant="neutral" className="bg-card">
      <Badge.Text>Powered by Speakeasy</Badge.Text>
    </Badge>
  );
};

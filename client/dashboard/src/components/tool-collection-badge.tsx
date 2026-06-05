import { cn } from "@/lib/utils";
import {
  Badge,
  Icon,
  Stack,
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@speakeasy-api/moonshine";

export const ToolCollectionBadge = ({
  toolNames,
  variant = "neutral",
  className,
  warnOnTooManyTools = false,
  emptyLabel,
}: {
  toolNames: string[] | undefined;
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
}) => {
  let tooltipContent: React.ReactNode = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {toolNames?.map((tool, i) => (
          <p key={i}>{tool}</p>
        ))}
      </Stack>
    </div>
  );

  const toolsWarnings =
    warnOnTooManyTools && toolNames && toolNames.length > 40;
  if (toolsWarnings) {
    tooltipContent = (
      <>
        LLM tool-use performance typically degrades with MCP server size.
        General industry standards recommend keeping MCP servers at around 40
        tools or fewer
      </>
    );
  }

  if (!toolNames || toolNames.length === 0) {
    if (emptyLabel === null) return null;
    if (emptyLabel !== undefined) return <>{emptyLabel}</>;
    return (
      <Badge variant={variant} className={className}>
        No Tools
      </Badge>
    );
  }

  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge
          variant={toolsWarnings ? "warning" : variant}
          className={cn(!toolsWarnings && "bg-card", className)}
        >
          {toolsWarnings && (
            <Badge.LeftIcon>
              <Icon name="triangle-alert" className="inline-block" />
            </Badge.LeftIcon>
          )}
          <Badge.Text>
            {toolNames.length} Tool
            {toolNames.length === 1 ? "" : "s"}
          </Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
};

export const PoweredBySpeakeasyBadge = () => {
  return (
    <Badge variant="neutral" className="bg-card">
      <Badge.Text>Powered by Speakeasy</Badge.Text>
    </Badge>
  );
};

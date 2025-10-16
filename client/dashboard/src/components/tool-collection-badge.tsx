import { promptNames } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import { ToolsetEntry } from "@gram/client/models/components";
import {
  Badge,
  Icon,
  Stack,
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@speakeasy-api/moonshine";

// Define minimal types for badge components
type ToolsetForBadge = Pick<ToolsetEntry, "name" | "slug" | "promptTemplates">;

export const ToolsetPromptsBadge = ({
  toolset,
  variant = "neutral",
}: {
  toolset: ToolsetForBadge | undefined;
  variant?: "neutral" | "destructive" | "information" | "success" | "warning";
}) => {
  const names = toolset ? promptNames(toolset.promptTemplates) : [];

  const tooltipContent = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {names.map((prompt, i) => (
          <p key={i}>{prompt}</p>
        ))}
      </Stack>
    </div>
  );

  return names && names.length > 0 ? (
    <Tooltip>
      <TooltipTrigger>
        <Badge variant={variant}>
          {names.length} Prompt{names.length === 1 ? "" : "s"}
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent side="bottom">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  ) : null;
};

export const ToolCollectionBadge = ({
  toolNames,
  variant = "neutral",
  className,
  warnOnTooManyTools = false,
}: {
  toolNames: string[] | undefined;
  variant?: React.ComponentProps<typeof Badge>["variant"];
  className?: string;
  warnOnTooManyTools?: boolean;
}) => {
  let tooltipContent: React.ReactNode = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {toolNames?.map((tool, i) => <p key={i}>{tool}</p>)}
      </Stack>
    </div>
  );

  const toolsWarnings =
    warnOnTooManyTools && toolNames && toolNames.length > 40;
  if (toolsWarnings) {
    tooltipContent = (
      <>
        LLM tool-use performance typically degrades with toolset size. General
        industry standards recommend keeping MCP servers at around 40 tools or
        fewer
      </>
    );
  }

  return toolNames && toolNames.length > 0 ? (
    <Tooltip>
      <TooltipTrigger>
        <Badge
          variant={toolsWarnings ? "warning" : variant}
          className={cn(
            !toolsWarnings && "bg-card",
            "flex items-center py-1 gap-1",
            className,
          )}
        >
          {toolsWarnings && (
            <Icon name="triangle-alert" className="inline-block" />
          )}
          <span>
            {toolNames.length} Tool{toolNames.length === 1 ? "" : "s"}
          </span>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  ) : (
    <Badge variant={variant} className={className}>
      No Tools
    </Badge>
  );
};

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
        <Badge variant={variant} className="bg-card">
          <Badge.Text>
            {names.length} Prompt{names.length === 1 ? "" : "s"}
          </Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent side="bottom">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  ) : null;
};

export const ResourcesBadge = ({
  resourceUris,
  variant = "neutral",
  className,
}: {
  resourceUris: string[] | undefined;
  variant?: React.ComponentProps<typeof Badge>["variant"];
  className?: string;
}) => {
  if (!resourceUris || resourceUris.length === 0) {
    return null;
  }

  const tooltipContent = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {resourceUris.map((uri, i) => (
          <p key={i}>{uri}</p>
        ))}
      </Stack>
    </div>
  );

  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge variant={variant} className={cn("bg-card", className)}>
          <Badge.Text>
            {resourceUris.length} Resource{resourceUris.length === 1 ? "" : "s"}
          </Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
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
        LLM tool-use performance typically degrades with toolset size. General
        industry standards recommend keeping MCP servers at around 40 tools or
        fewer
      </>
    );
  }

  if (!toolNames || toolNames.length === 0) {
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
            {toolNames.length} Tool{toolNames.length === 1 ? "" : "s"}
          </Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
};

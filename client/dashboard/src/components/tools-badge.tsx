import { ToolsetEntry } from "@gram/client/models/components";
import {
  Stack,
  Badge,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipPortal
} from "@speakeasy-api/moonshine";
import { UrgentWarningIcon } from "./ui/urgent-warning-icon";
import { cn } from "@/lib/utils";
import { promptNames } from "@/lib/toolNames";

// Define minimal types for badge components
type ToolsetForBadge = Pick<
  ToolsetEntry,
  "name" | "slug" | "httpTools" | "promptTemplates"
>;

export const ToolsetPromptsBadge = ({
  toolset,
  size = "md",
  variant = "outline",
}: {
  toolset: ToolsetForBadge | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
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
        <Badge size={size} variant={variant}>
          {names.length} Prompt{names.length === 1 ? "" : "s"}
        </Badge>
      </TooltipTrigger>
      <TooltipContent side="bottom">{tooltipContent}</TooltipContent>
    </Tooltip>
  ) : null;
};

export const ToolsBadge = ({
  toolNames,
  size = "md",
  variant = "outline",
  className,
  warnOnTooManyTools = false,
}: {
  toolNames: string[] | undefined;
  size?: "sm" | "md";
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
    tooltipContent =
      "LLM tool-use performance typically degrades with toolset size. General industry standards recommend keeping MCP servers at around 40 tool or fewer";
  }

  return toolNames && toolNames.length > 0 ? (
    <Tooltip>
      <TooltipTrigger>
        <Badge
          size={size}
          variant={toolsWarnings ? "warning" : variant}
          className={cn(!toolsWarnings && "bg-card", 'flex items-center py-1 gap-[1ch]',className)}
        >
            {toolsWarnings && <UrgentWarningIcon className="inline-block" />}
            {toolNames.length} Tool{toolNames.length === 1 ? "" : "s"}
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  ) : (
    <Badge size={size} variant={variant} className={className}>
      No Tools
    </Badge>
  );
};

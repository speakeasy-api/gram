import { ToolsetEntry } from "@gram/client/models/components";
import {
  Stack,
  Badge,
  Tooltip,
  TooltipTrigger,
  TooltipProvider,
  TooltipContent
} from "@speakeasy-api/moonshine";
import { UrgentWarningIcon } from "./ui/urgent-warning-icon";
import { cn } from "@/lib/utils";
import { higherOrderToolNames, promptNames } from "@/lib/toolNames";

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
    <TooltipProvider>
    
      <TooltipTrigger>
        
    <Badge size={size} variant={variant} >
      {names.length} Prompt{names.length === 1 ? "" : "s"}
    </Badge>
  </TooltipTrigger>
  </TooltipProvider>
  ) : null;
};

function httpToolNames(toolset: ToolsetForBadge) {
  const { httpTools } = toolset;

  return httpTools.map((tool) => tool.name);
}

function toolNames(toolset: ToolsetForBadge) {
  const { promptTemplates } = toolset;

  return httpToolNames(toolset).concat(higherOrderToolNames(promptTemplates));
}

export const ToolsetToolsBadge = ({
  toolset,
  size = "md",
  variant = "outline",
  className,
}: {
  toolset: ToolsetForBadge | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
  className?: string;
}) => {
  const names: string[] = toolset ? toolNames(toolset) : [];
  return (
    <ToolsBadge
      toolNames={names}
      size={size}
      variant={variant}
      className={className}
      warnOnTooManyTools
    />
  );
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
  variant?: "outline" | "default";
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
    warnOnTooManyTools &&
    toolNames &&
    toolNames.length > 40 &&
    toolNames.length < 150;
  if (toolsWarnings) {
    tooltipContent =
      "LLM tool-use performance typically degrades with toolset size. General industry standards recommend keeping MCP servers at around 40 tool or fewer";
  }

  const toolsSevere = warnOnTooManyTools && toolNames && toolNames.length > 150;
  if (toolsSevere) {
    tooltipContent =
      "An LLM is unlikely to consistently perform well with a toolset of this size. General industry standards recommend keeping MCP servers at around 40 tool or fewer";
  }

  const anyWarnings = toolsWarnings || toolsSevere;

  return toolNames && toolNames.length > 0 ? (
    <TooltipProvider>
      <Tooltip>
      <TooltipTrigger>
        <Badge
          size={size}
          variant={"warning"}
          className={cn(!anyWarnings && "bg-card", className)}
        >
          {anyWarnings && (
            <UrgentWarningIcon
              className={toolsSevere ? "text-white dark:text-white" : undefined}
            />
          )}
          {toolNames.length} Tool{toolNames.length === 1 ? "" : "s"}
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{tooltipContent}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    <Badge size={size} variant={variant} className={className}>
      No Tools
    </Badge>
  );
};

import { useGroupedToolDefinitions } from "@/lib/toolNames";
import { getToolsetPrompts } from "@/pages/prompts/Prompts";
import { Toolset } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { Badge } from "./ui/badge";

export const ToolsetPromptsBadge = ({
  toolset,
  size = "md",
  variant = "default",
}: {
  toolset: Toolset | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
}) => {
  const promptNames = getToolsetPrompts(toolset)?.map((prompt) => prompt.name);

  const tooltipContent = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {promptNames?.map((prompt, i) => (
          <p key={i}>{prompt}</p>
        ))}
      </Stack>
    </div>
  );

  return promptNames && promptNames.length > 0 ? (
    <Badge size={size} variant={variant} tooltip={tooltipContent}>
      {promptNames.length} Prompt{promptNames.length === 1 ? "" : "s"}
    </Badge>
  ) : null;
};

export const ToolsetToolsBadge = ({
  toolset,
  size = "md",
  variant = "default",
  className,
}: {
  toolset: Toolset | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
  className?: string;
}) => {
  const groupedTools = useGroupedToolDefinitions(toolset);

  const groupedToolNames =
    groupedTools.length == 1
      ? groupedTools[0]!.tools.map((tool) => tool.displayName)
      : groupedTools.flatMap((group) => group.tools.map((tool) => tool.name));

  groupedToolNames.push(
    ...(toolset?.promptTemplates ?? []).map((template) => template.name)
  );

  return (
    <ToolsBadge
      toolNames={groupedToolNames}
      size={size}
      variant={variant}
      className={className}
    />
  );
};

export const ToolsBadge = ({
  toolNames,
  size = "md",
  variant = "default",
  className,
}: {
  toolNames: string[] | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
  className?: string;
}) => {
  const tooltipContent = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {toolNames?.map((tool, i) => (
          <p key={i}>{tool}</p>
        ))}
      </Stack>
    </div>
  );

  return toolNames && toolNames.length > 0 ? (
    <Badge
      size={size}
      variant={variant}
      tooltip={tooltipContent}
      className={className}
    >
      {toolNames.length} Tool{toolNames.length === 1 ? "" : "s"}
    </Badge>
  ) : (
    <Badge size={size} variant={variant} className={className}>
      No Tools
    </Badge>
  );
};

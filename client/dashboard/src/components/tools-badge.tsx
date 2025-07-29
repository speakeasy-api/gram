import { useGroupedToolDefinitions } from "@/lib/toolNames";
import { getToolsetPrompts } from "@/pages/prompts/Prompts";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { Badge, TwoPartBadge } from "./ui/badge";
import { UrgentWarningIcon } from "./ui/urgent-warning-icon";
import { cn } from "@/lib/utils";

export const ToolsetBadge = ({
  toolset,
  size = "md",
}: {
  toolset: Toolset | undefined;
  size?: "sm" | "md";
}) => {
  const routes = useRoutes();

  if (!toolset) {
    return <Badge size={size} variant="outline" isLoading />;
  }

  return (
    <TwoPartBadge>
      <routes.toolsets.toolset.Link params={[toolset.slug]}>
        <Badge className="capitalize">{toolset.name}</Badge>
      </routes.toolsets.toolset.Link>
      <ToolsetToolsBadge
        toolset={toolset}
        variant="outline"
        className="lowercase"
      />
    </TwoPartBadge>
  );
};

export const ToolsetPromptsBadge = ({
  toolset,
  size = "md",
  variant = "outline",
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
  variant = "outline",
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

  const toolsWarnings = warnOnTooManyTools && toolNames && toolNames.length > 40 && toolNames.length < 150;
  if (toolsWarnings) {
    tooltipContent =
      "LLM tool-use performance typically degrades with toolset size. General industry standards recommend keeping MCP servers at around 40 tool or less";
  }

  const toolsSevere = warnOnTooManyTools && toolNames && toolNames.length > 150;
  if (toolsSevere) {
    tooltipContent =
      "An LLM is unlikely to consistently perform well with a toolset of this size. General industry standards recommend keeping MCP servers at around 40 tool or less";
  }

  return toolNames && toolNames.length > 0 ? (
    <Badge
      size={size}
      variant={toolsSevere ? "urgent-warning" : toolsWarnings ? "warning" : variant}
      tooltip={tooltipContent}
      className={cn(!toolsSevere && !toolsWarnings && "bg-card", className)}
    >
      {(toolsWarnings || toolsSevere) && (
        <UrgentWarningIcon 
          className={toolsSevere ? "text-white dark:text-white" : undefined}
        />
      )}
      {toolNames.length} Tool{toolNames.length === 1 ? "" : "s"}
    </Badge>
  ) : (
    <Badge size={size} variant={variant} className={className}>
      No Tools
    </Badge>
  );
};

import { useGroupedTools } from "@/lib/toolNames";
import { Toolset } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { Badge } from "./ui/badge";

export const ToolsetToolsBadge = ({
  toolset,
  size = "md",
  variant = "default",
}: {
  toolset: Toolset | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
}) => {
  const groupedTools = useGroupedTools(toolset?.httpTools ?? []);

  const groupedToolNames =
    groupedTools.length == 1
      ? groupedTools[0]!.tools.map((tool) => tool.displayName)
      : groupedTools.flatMap((group) => group.tools.map((tool) => tool.name));

  groupedToolNames.push(
    ...(toolset?.promptTemplates ?? []).map((template) => template.name)
  );

  return (
    <ToolsBadge toolNames={groupedToolNames} size={size} variant={variant} />
  );
};

export const ToolsBadge = ({
  toolNames,
  size = "md",
  variant = "default",
}: {
  toolNames: string[] | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
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
    <Badge size={size} variant={variant} tooltip={tooltipContent}>
      {toolNames.length} Tool{toolNames.length === 1 ? "" : "s"}
    </Badge>
  ) : (
    <Badge size={size} variant={variant}>
      No Tools
    </Badge>
  );
};

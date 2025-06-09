import { useGroupedTools } from "@/lib/toolNames";
import { HTTPToolDefinition } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { Badge } from "./ui/badge";

export const ToolsBadge = ({
  tools,
  size = "md",
  variant = "default",
}: {
  tools: (HTTPToolDefinition | string)[] | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
}) => {
  const isStrings = tools?.every((tool) => typeof tool === "string");
  const groupedTools = useGroupedTools(
    !isStrings && tools ? (tools as HTTPToolDefinition[]) : []
  );

  const groupedToolNames =
    groupedTools.length == 1
      ? groupedTools[0]!.tools.map((tool) => tool.displayName)
      : groupedTools.flatMap((group) => group.tools.map((tool) => tool.name));

  const toolNames = isStrings ? tools : groupedToolNames;

  const tooltipContent = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {toolNames.map((tool, i) => (
          <p key={i}>{tool}</p>
        ))}
      </Stack>
    </div>
  );

  return tools && tools.length > 0 ? (
    <Badge size={size} variant={variant} tooltip={tooltipContent}>
      {tools.length} Tool{tools.length === 1 ? "" : "s"}
    </Badge>
  ) : (
    <Badge size={size} variant={variant}>
      No Tools
    </Badge>
  );
};

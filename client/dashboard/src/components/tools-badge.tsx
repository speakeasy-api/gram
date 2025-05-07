import { Stack } from "@speakeasy-api/moonshine";
import {
  TooltipProvider,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "./ui/tooltip";
import { Badge } from "./ui/badge";
import { reduceToolNames as simplifyToolNames } from "@/lib/toolNames";

export const ToolsBadge = ({
  tools,
  size = "sm",
  variant = "default",
}: {
  tools: ({ name: string } | string)[] | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
}) => {
  const { tools: cleanedTools } = simplifyToolNames(
    tools?.map((tool) => (typeof tool === "string" ? { name: tool } : tool)) ??
      []
  );

  const tooltipContent = (
    <div className="max-h-[300px] overflow-y-auto">
      <Stack gap={1}>
        {cleanedTools.map((tool, i) => (
          <p key={i}>{tool.displayName}</p>
        ))}
      </Stack>
    </div>
  );

  return tools && tools.length > 0 ? (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge size={size} variant={variant}>
            {tools.length} Tool{tools.length === 1 ? "" : "s"}
          </Badge>
        </TooltipTrigger>
        <TooltipContent>{tooltipContent}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    <Badge size={size} variant={variant}>
      No Tools
    </Badge>
  );
};

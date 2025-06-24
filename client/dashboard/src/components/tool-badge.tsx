import { ToolDefinition } from "@/pages/toolsets/types";
import { Stack } from "@speakeasy-api/moonshine";
import { HttpRoute } from "./http-route";
import { Badge } from "./ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./ui/tooltip";
import { Type } from "./ui/type";

export function ToolBadge({
  tool,
  variant = "secondary",
}: {
  tool: ToolDefinition;
  variant?: "default" | "secondary";
}) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge variant={variant} size="sm">{tool.name}</Badge>
        </TooltipTrigger>
        <TooltipContent inverted>
          <Stack className="max-w-md">
            {tool.type === "http" && (
              <HttpRoute method={tool.httpMethod} path={tool.path} />
            )}
            <Type>{tool.description}</Type>
          </Stack>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

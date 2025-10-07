import { Stack } from "@speakeasy-api/moonshine";
import { HttpRoute } from "./http-route";
import { Badge } from "./ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { Type } from "./ui/type";
import { Tool } from "@/lib/toolTypes";

export function ToolBadge({
  tool,
  variant = "secondary",
  className,
}: {
  tool: Tool;
  variant?: "default" | "secondary" | "outline";
  className?: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant={variant} size="sm" className={className}>
          {tool.name}
        </Badge>
      </TooltipTrigger>
      <TooltipContent inverted>
        <Stack className="max-w-md pt-2" gap={1}>
          {tool.type === "http" && (
            <HttpRoute method={tool.httpMethod} path={tool.path} />
          )}
          <Type small className="line-clamp-3">
            {tool.description}
          </Type>
        </Stack>
      </TooltipContent>
    </Tooltip>
  );
}

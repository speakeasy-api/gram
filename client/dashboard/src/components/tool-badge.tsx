import { StandardTool } from "@/lib/toolTypes";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { HttpRoute } from "./http-route";
import { Badge } from "./ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { Type } from "./ui/type";

export function ToolBadge({
  tool,
  variant = "secondary",
  className,
}: {
  tool: StandardTool;
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
        <Stack className="max-w-md" gap={1}>
          {tool.type === "http" && (
            <HttpRoute
              method={tool.httpMethod}
              path={tool.path}
              className="pt-2"
            />
          )}
          {tool.type === "function" && (
            <Stack direction="horizontal" gap={1} align="end">
              <Icon
                name="square-function"
                size="small"
                className="text-muted-foreground"
              />
              <Type small mono muted>
                {tool.name}
              </Type>
            </Stack>
          )}
          <Type small className="line-clamp-3">
            {tool.description}
          </Type>
        </Stack>
      </TooltipContent>
    </Tooltip>
  );
}

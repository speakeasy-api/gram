import { Tool } from "@/lib/toolTypes";
import { Stack } from "@/components/ui/stack";
import { Badge } from "@/components/ui/badge";
import { SquareFunction } from "lucide-react";
import { HttpRoute } from "./http-route";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { Type } from "./ui/type";

export function ToolBadge({
  tool,
  variant = "secondary",
  className,
}: {
  tool: Tool;
  variant?: "default" | "secondary" | "outline";
  className?: string;
}): JSX.Element {
  // "secondary"/"outline" both mapped to the old local Badge's translucent
  // treatment; moonshine's neutral variant only distinguishes via `background`.
  const background = variant === "default";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge
          variant="neutral"
          background={background}
          size="sm"
          className={className}
        >
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
              <SquareFunction className="text-muted-foreground size-4" />
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

import { Tool } from "@/lib/toolTypes";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { HttpRoute } from "./http-route";
import { Badge, type BadgeProps } from "./ui/Badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/Tooltip";
import { Text } from "@/components/ui/Text";

export function ToolBadge({
  tool,
  variant = "neutral",
  className,
}: {
  tool: Tool;
  variant?: BadgeProps["variant"];
  className?: string;
}): JSX.Element {
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
              <Text small mono muted>
                {tool.name}
              </Text>
            </Stack>
          )}
          <Text small className="line-clamp-3">
            {tool.description}
          </Text>
        </Stack>
      </TooltipContent>
    </Tooltip>
  );
}

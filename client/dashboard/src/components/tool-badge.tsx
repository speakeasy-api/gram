import { HTTPToolDefinition } from "@gram/client/models/components";
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

export function ToolBadge({ tool }: { tool: HTTPToolDefinition }) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge>{tool.name}</Badge>
        </TooltipTrigger>
        <TooltipContent inverted>
          <Stack className="max-w-md">
            <HttpRoute method={tool.httpMethod} path={tool.path} />
            <Type>{tool.description}</Type>
          </Stack>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

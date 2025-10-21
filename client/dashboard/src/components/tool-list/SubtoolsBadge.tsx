import { Tool } from "@/lib/toolTypes";
import {
  Badge,
  Stack,
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@speakeasy-api/moonshine";

export function SubtoolsBadge({
  tool,
  availableToolUrns,
}: {
  tool: Tool;
  availableToolUrns: string[];
}) {
  if (tool.type !== "prompt") return null;
  if (tool.toolsHint.length === 0) return null;

  const nameFromUrn = (urn: string) => {
    const split = urn.split(":");
    return split[split.length - 1];
  };

  let presentToolNames: string[] = [];
  let missingToolNames: string[] = [];
  if (tool.toolUrnsHint) {
    const presentToolUrns = availableToolUrns.filter(
      (urn) => tool.toolUrnsHint?.includes(urn) ?? false,
    );
    const missingToolUrns =
      tool.toolUrnsHint?.filter((urn) => !availableToolUrns.includes(urn)) ??
      [];

    presentToolNames = presentToolUrns.map((urn) => nameFromUrn(urn));
    missingToolNames = missingToolUrns.map((urn) => nameFromUrn(urn));
  } else {
    const availableToolNames = availableToolUrns.map((urn) => nameFromUrn(urn));
    presentToolNames = tool.toolsHint.filter((name) =>
      availableToolNames.includes(name),
    );
    missingToolNames = tool.toolsHint.filter(
      (name) => !availableToolNames.includes(name),
    );
  }

  const hasAllSubtools = missingToolNames.length === 0;

  const tooltipContent = (
    <Stack gap={1}>
      {missingToolNames.map((name) => (
        <span key={name}>
          <span className="text-muted-foreground">✗</span> {name}
        </span>
      ))}
      {presentToolNames.map((name) => (
        <span key={name}>
          <span className="text-muted-foreground">✓</span> {name}
        </span>
      ))}
    </Stack>
  );

  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge
          className="font-mono text-xs"
          variant={hasAllSubtools ? "success" : "warning"}
        >
          <Badge.Text>
            {hasAllSubtools
              ? `${tool.toolsHint.length} subtool${tool.toolsHint.length === 1 ? "" : "s"}`
              : "Missing subtools"}
          </Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent>{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
}

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { ProxiedMcpToolAnnotations } from "@/hooks/useProxiedMcpTools";
import { cn } from "@/lib/utils";
import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import { AlertTriangle, Eye, Globe, Repeat } from "lucide-react";

const ANNOTATIONS = [
  { key: "readOnlyHint", label: "Read-only", Icon: Eye },
  { key: "destructiveHint", label: "Destructive", Icon: AlertTriangle },
  { key: "idempotentHint", label: "Idempotent", Icon: Repeat },
  { key: "openWorldHint", label: "Open world", Icon: Globe },
] as const;

/**
 * The MCP annotation hints a tool actually asserts, one icon each.
 *
 * Unlike the shared AnnotationBadgeIcons — which shows only the most salient
 * hint and drops open-world entirely to keep dense tool lists readable — every
 * hint the tool states is rendered here, in a stable order.
 *
 * Hints are tri-state and the distinction is load-bearing on this tab: `false`
 * is the server asserting something, whereas unset is it declining to say. An
 * unset hint is omitted so the row only carries claims that were actually made,
 * and the remaining two states are drawn differently (solid / muted) with the
 * value named in the tooltip, since that difference is too fine to carry on
 * styling alone.
 */
export function ToolAnnotationIndicators({
  annotations,
  stored,
}: {
  annotations?: ProxiedMcpToolAnnotations;
  /** Stored metadata wins over what the session advertises, when present. */
  stored?: ToolMetadata;
}): JSX.Element | null {
  const asserted = ANNOTATIONS.map((annotation) => ({
    ...annotation,
    // Stored metadata wins, and only `undefined` falls through — a stored
    // `false` is an assertion in its own right.
    value: stored?.[annotation.key] ?? annotations?.[annotation.key],
  })).filter((annotation) => annotation.value !== undefined);

  if (asserted.length === 0) return null;

  return (
    <span className="flex shrink-0 items-center gap-1">
      {asserted.map(({ key, label, Icon, value }) => {
        const description = `${label}: ${String(value)}`;

        return (
          <Tooltip key={key}>
            <TooltipTrigger asChild>
              <span aria-label={description} className="flex">
                <Icon
                  aria-hidden
                  strokeWidth={value === true ? 2.5 : 1.5}
                  className={cn(
                    "size-3.5",
                    value === true
                      ? "text-foreground"
                      : "text-muted-foreground",
                  )}
                />
              </span>
            </TooltipTrigger>
            <TooltipContent>{description}</TooltipContent>
          </Tooltip>
        );
      })}
    </span>
  );
}

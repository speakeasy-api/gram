import { toolSupportsAnnotations, type Tool } from "@/lib/toolTypes";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Shield, AlertTriangle, Repeat, Globe } from "lucide-react";

export interface ResolvedToolAnnotations {
  readOnly: boolean;
  destructive: boolean;
  idempotent: boolean;
  openWorld: boolean;
}

/**
 * Resolves annotation hints for a tool by merging the base annotations with
 * any variation overrides. Variation values take precedence when present;
 * unset (nullish) variation fields fall through to the base annotation value.
 *
 * Returns null for tool types that don't carry annotations (prompt, external-mcp).
 */
function resolveToolAnnotations(tool: Tool): ResolvedToolAnnotations | null {
  if (!toolSupportsAnnotations(tool)) return null;

  const base = tool.annotations;
  const override = tool.variation;

  return {
    readOnly: Boolean(override?.readOnlyHint ?? base?.readOnlyHint),
    destructive: Boolean(override?.destructiveHint ?? base?.destructiveHint),
    idempotent: Boolean(override?.idempotentHint ?? base?.idempotentHint),
    openWorld: Boolean(override?.openWorldHint ?? base?.openWorldHint),
  };
}

export function AnnotationBadges({ tool }: { tool: Tool }): JSX.Element | null {
  const annotations = resolveToolAnnotations(tool);
  if (!annotations) return null;
  return <AnnotationBadgeIcons {...annotations} />;
}

/**
 * Presentational annotation-hint icons, decoupled from the Gram `Tool` model so
 * other tool sources (e.g. remote MCP servers) can render the same badges from
 * their own resolved hints. Returns null when no hint is set.
 */
export function AnnotationBadgeIcons({
  readOnly,
  destructive,
  idempotent,
  openWorld,
}: ResolvedToolAnnotations): JSX.Element | null {
  if (!readOnly && !destructive && !idempotent && !openWorld) return null;

  return (
    <div className="flex shrink-0 items-center gap-1">
      {readOnly && (
        <SimpleTooltip tooltip="Read-only">
          <Shield className="text-muted-foreground/70 size-3.5" />
        </SimpleTooltip>
      )}
      {destructive && !readOnly && (
        <SimpleTooltip tooltip="Destructive">
          <AlertTriangle className="text-muted-foreground/70 size-3.5" />
        </SimpleTooltip>
      )}
      {idempotent && !readOnly && (
        <SimpleTooltip tooltip="Idempotent">
          <Repeat className="text-muted-foreground/70 size-3.5" />
        </SimpleTooltip>
      )}
      {openWorld && (
        <SimpleTooltip tooltip="Open-world">
          <Globe className="text-muted-foreground/70 size-3.5" />
        </SimpleTooltip>
      )}
    </div>
  );
}

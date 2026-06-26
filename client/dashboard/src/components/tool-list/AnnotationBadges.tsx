import { toolSupportsAnnotations, type Tool } from "@/lib/toolTypes";
import { Badge } from "@speakeasy-api/moonshine";

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
 * Presentational annotation-hint labels, decoupled from the Gram `Tool` model so
 * other tool sources (e.g. remote MCP servers) can render the same badges from
 * their own resolved hints. Returns null when no hint is set.
 *
 * Renders the same text labels and variants as the Connect → Catalog → MCP tool
 * cards (`CatalogDetail`), so the permission labels read identically wherever a
 * tool is surfaced — including Distribute → MCP → Tools.
 */
export function AnnotationBadgeIcons({
  readOnly,
  destructive,
  idempotent,
  openWorld,
}: ResolvedToolAnnotations): JSX.Element | null {
  if (!readOnly && !destructive && !idempotent && !openWorld) return null;

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-1">
      {readOnly && (
        <Badge variant="neutral" className="text-xs">
          Read-only
        </Badge>
      )}
      {destructive && !readOnly && (
        <Badge variant="warning" className="text-xs">
          Destructive
        </Badge>
      )}
      {idempotent && !readOnly && (
        <Badge variant="information" className="text-xs">
          Idempotent
        </Badge>
      )}
      {openWorld && (
        <Badge variant="neutral" className="text-xs">
          Open-world
        </Badge>
      )}
    </div>
  );
}

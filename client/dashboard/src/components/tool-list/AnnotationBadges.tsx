import type { Tool } from "@/lib/toolTypes";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Shield, AlertTriangle, Repeat, Globe } from "lucide-react";

interface EffectiveAnnotations {
  readOnly: boolean;
  destructive: boolean;
  idempotent: boolean;
  openWorld: boolean;
}

/**
 * Extracts the effective annotation hints for a tool, merging base tool
 * annotations with variation overrides (variation wins when present).
 *
 * Only http and function tools support annotations per the MCP spec.
 * The annotation fields are nullable booleans on both the tool and its
 * variation -- NULL means "not set" / inherit from base.
 */
function getEffectiveAnnotations(tool: Tool): EffectiveAnnotations | null {
  if (tool.type !== "http" && tool.type !== "function") return null;

  // The annotation hint fields may not yet be present on the SDK types
  // (they are being added as part of AGE-1348). Access them defensively.
  const t = tool as Record<string, unknown>;
  const v = (t.variation ?? {}) as Record<string, unknown>;

  const readOnly = Boolean(v.readOnlyHint ?? t.readOnlyHint ?? false);
  const destructive = Boolean(
    v.destructiveHint ?? t.destructiveHint ?? false,
  );
  const idempotent = Boolean(
    v.idempotentHint ?? t.idempotentHint ?? false,
  );
  const openWorld = Boolean(v.openWorldHint ?? t.openWorldHint ?? false);

  return { readOnly, destructive, idempotent, openWorld };
}

export function AnnotationBadges({ tool }: { tool: Tool }) {
  const annotations = getEffectiveAnnotations(tool);
  if (!annotations) return null;

  const { readOnly, destructive, idempotent, openWorld } = annotations;
  if (!readOnly && !destructive && !idempotent && !openWorld) return null;

  return (
    <div className="flex items-center gap-1 shrink-0">
      {readOnly && (
        <SimpleTooltip tooltip="Read-only">
          <Shield className="size-3.5 text-muted-foreground/70" />
        </SimpleTooltip>
      )}
      {destructive && !readOnly && (
        <SimpleTooltip tooltip="Destructive">
          <AlertTriangle className="size-3.5 text-muted-foreground/70" />
        </SimpleTooltip>
      )}
      {idempotent && !readOnly && (
        <SimpleTooltip tooltip="Idempotent">
          <Repeat className="size-3.5 text-muted-foreground/70" />
        </SimpleTooltip>
      )}
      {openWorld && (
        <SimpleTooltip tooltip="Open-world">
          <Globe className="size-3.5 text-muted-foreground/70" />
        </SimpleTooltip>
      )}
    </div>
  );
}

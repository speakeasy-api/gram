import type { Selector } from "@gram/client/models/components/selector.js";

import { ANNOTATION_TO_DISPOSITION } from "./types";

/**
 * Selector transitions for the tool-selection panel's annotation → manual
 * mode switch. The panel promises the two modes are mutually exclusive, so
 * entering manual mode must atomically DROP every category-wide disposition
 * selector while applying the tool change — sequencing two state updates
 * would leave the annotation scope persisted alongside the picked tool,
 * silently widening the grant.
 */

function withoutDispositions(selectors: Selector[]): Selector[] {
  return selectors.filter((s) => !s.disposition);
}

export function selectorsAfterToolToggle(
  selectors: Selector[],
  serverId: string,
  toolName: string,
): Selector[] {
  const base = withoutDispositions(selectors);
  const exists = base.some(
    (s) => s.resourceId === serverId && s.tool === toolName,
  );
  if (exists) {
    return base.filter(
      (s) => !(s.resourceId === serverId && s.tool === toolName),
    );
  }
  return [
    ...base,
    { resourceKind: "mcp" as const, resourceId: serverId, tool: toolName },
  ];
}

export function selectorsAfterToolBatch(
  selectors: Selector[],
  serverId: string,
  toolNames: string[],
  selected: boolean,
): Selector[] {
  const base = withoutDispositions(selectors);
  if (selected) {
    const existing = new Set(
      base
        .filter((s) => s.resourceId === serverId && s.tool)
        .map((s) => s.tool!),
    );
    const toAdd = toolNames
      .filter((name) => !existing.has(name))
      .map((name) => ({
        resourceKind: "mcp" as const,
        resourceId: serverId,
        tool: name,
      }));
    return [...base, ...toAdd];
  }
  const toolSet = new Set(toolNames);
  return base.filter(
    (s) => !(s.resourceId === serverId && s.tool && toolSet.has(s.tool)),
  );
}

const PANEL_DISPOSITIONS: ReadonlySet<string> = new Set(
  Object.values(ANNOTATION_TO_DISPOSITION),
);

/**
 * The selectors after an annotation is ticked or unticked in the panel. The
 * panel shows only the four annotation hints, so a stored disposition it
 * cannot show, such as unannotated, is kept as it was instead of being
 * dropped by an edit to another annotation.
 */
export function selectorsAfterAnnotationToggle(
  selectors: Selector[],
  dispositions: NonNullable<Selector["disposition"]>[],
): Selector[] {
  const kept = selectors.filter(
    (s) => s.disposition && !PANEL_DISPOSITIONS.has(s.disposition),
  );
  return [
    ...dispositions.map((disposition) => ({
      resourceKind: "mcp" as const,
      resourceId: "*",
      disposition,
    })),
    ...kept,
  ];
}

import type { Selector } from "@gram/client/models/components/selector.js";

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

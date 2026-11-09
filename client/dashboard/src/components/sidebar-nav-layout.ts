import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeGatedNavEntry } from "./scope-gated-nav-group";

/** The entries the user holds a scope for, in their original order. */
export function visibleNavEntries(
  items: ScopeGatedNavEntry[],
  hasAnyScope: (
    scopes: Scope[],
    resourceId?: string,
    projectId?: string,
  ) => boolean,
): ScopeGatedNavEntry[] {
  return items.filter((entry) => {
    if (entry.scope === undefined) return true;
    const scopes = Array.isArray(entry.scope) ? entry.scope : [entry.scope];
    return hasAnyScope(scopes, entry.resourceId, entry.projectId);
  });
}

/**
 * Decides how the grouped part of the sidebar renders once scopes are known.
 *
 * Groups the user can open nothing in are dropped. A lone remaining group has
 * nothing to be told apart from, so its header and collapse toggle only cost a
 * click: its items are returned flat instead. The divider separating the
 * top-level items from the groups is drawn only with something on both sides.
 */
export function sidebarNavLayout<G extends { items: ScopeGatedNavEntry[] }>(
  groups: G[],
  hasTopItems: boolean,
  hasAnyScope: Parameters<typeof visibleNavEntries>[1],
): {
  groups: G[];
  flatEntries: ScopeGatedNavEntry[] | undefined;
  showDivider: boolean;
} {
  const visible = groups.filter(
    (group) => visibleNavEntries(group.items, hasAnyScope).length > 0,
  );
  const lone = visible.length === 1 ? visible[0] : undefined;
  return {
    groups: visible,
    flatEntries: lone ? visibleNavEntries(lone.items, hasAnyScope) : undefined,
    showDivider: hasTopItems && visible.length > 0,
  };
}

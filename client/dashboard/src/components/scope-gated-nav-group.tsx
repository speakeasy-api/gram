import { CollapsibleNavGroup, CollapsibleNavItem } from "@/components/nav-menu";
import { ReleaseStage } from "@/components/release-stage-badge";
import { useRBAC } from "@/hooks/useRBAC";
import { AppRoute } from "@/routes";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import React from "react";

/** A nav sub-item plus the scopes that make it visible. */
export interface ScopeGatedNavEntry {
  item: AppRoute;
  /** Any one scope grants visibility. Omit for items gated by something else. */
  scope?: Scope | Scope[];
  /** Optional resource ID to check the scopes against. */
  resourceId?: string;
  /** Display text override; see `CollapsibleNavItem`'s `label`. */
  label?: string;
}

/**
 * A collapsible sidebar group whose items are scope-gated as data rather than
 * by wrapping each child in `RequireScope`.
 *
 * Gating per child can't hide the group itself — the parent has no way to know
 * its children rendered nothing — so a user with none of the group's scopes
 * still saw an empty, expandable group. Filtering up front lets the group
 * disappear entirely, and lets the group header link to the first item the user
 * can actually open instead of a fixed (possibly forbidden) page.
 */
export function ScopeGatedNavGroup({
  label,
  Icon,
  items,
  stage,
}: {
  label: string;
  Icon: React.ComponentType<{ className?: string }>;
  items: ScopeGatedNavEntry[];
  stage?: ReleaseStage;
}): React.JSX.Element | null {
  const { hasAnyScope } = useRBAC();

  const visible = items.filter((entry) => {
    if (entry.scope === undefined) return true;
    const scopes = Array.isArray(entry.scope) ? entry.scope : [entry.scope];
    return hasAnyScope(scopes, entry.resourceId);
  });

  const first = visible[0];
  if (!first) return null;

  return (
    <CollapsibleNavGroup
      label={label}
      Icon={Icon}
      defaultHref={first.item.href()}
      stage={stage}
    >
      {visible.map((entry) => (
        <CollapsibleNavItem
          key={entry.item.url}
          item={entry.item}
          label={entry.label}
        />
      ))}
    </CollapsibleNavGroup>
  );
}

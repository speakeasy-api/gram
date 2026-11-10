import {
  CollapsibleNavGroup,
  CollapsibleNavItem,
  ReleaseStageNavSection,
} from "@/components/nav-menu";
import { ReleaseStage } from "@/components/release-stage-badge";

// Stable items lead each group; staged items follow, most mature first.
const STAGE_ORDER: ReleaseStage[] = ["beta", "preview"];
import { useRBAC } from "@/hooks/useRBAC";
import { AppRoute } from "@/routes";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import React from "react";
import { visibleNavEntries } from "./sidebar-nav-layout";

/** A nav sub-item plus the scopes that make it visible. */
export interface ScopeGatedNavEntry {
  item: AppRoute;
  /** Any one scope grants visibility. Omit for items gated by something else. */
  scope?: Scope | Scope[];
  /** Optional resource ID to check the scopes against. */
  resourceId?: string;
  /** Optional project the resource belongs to, for project-wide grants. */
  projectId?: string;
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

  const visible = visibleNavEntries(items, hasAnyScope);

  const first = visible[0];
  if (!first) return null;

  return (
    <CollapsibleNavGroup
      label={label}
      Icon={Icon}
      defaultHref={first.item.href()}
      stage={stage}
    >
      {visible
        .filter((entry) => !entry.item.stage)
        .map((entry) => (
          <CollapsibleNavItem
            key={entry.item.url}
            item={entry.item}
            label={entry.label}
          />
        ))}
      {STAGE_ORDER.map((sectionStage) => {
        const staged = visible.filter(
          (entry) => entry.item.stage === sectionStage,
        );
        if (staged.length === 0) return null;
        return (
          <ReleaseStageNavSection key={sectionStage} stage={sectionStage}>
            {staged.map((entry) => (
              <CollapsibleNavItem
                key={entry.item.url}
                item={entry.item}
                label={entry.label}
                stageSection={sectionStage}
              />
            ))}
          </ReleaseStageNavSection>
        );
      })}
    </CollapsibleNavGroup>
  );
}

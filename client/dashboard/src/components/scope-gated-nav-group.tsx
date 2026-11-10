import {
  CollapsibleNavGroup,
  CollapsibleNavItem,
  ReleaseStageNavSection,
} from "@/components/nav-menu";
import type { ReleaseStage } from "@/components/release-stage-badge";

// Stable items lead each group; staged items follow, most mature first. Keyed
// by stage so a new stage fails to compile until it is given a place here —
// otherwise its items would match neither list and vanish from the group.
const STAGE_SECTIONS: Record<ReleaseStage, true> = {
  beta: true,
  preview: true,
};
const STAGE_ORDER = Object.keys(STAGE_SECTIONS) as ReleaseStage[];
import { useRBAC } from "@/hooks/useRBAC";
import type { AppRoute } from "@/routes";
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
  const stable = visible.filter((entry) => !entry.item.stage);
  const sections = STAGE_ORDER.map((sectionStage) => ({
    sectionStage,
    entries: visible.filter((entry) => entry.item.stage === sectionStage),
  })).filter((section) => section.entries.length > 0);

  // The header links to the top row as rendered: stable items first, then
  // each stage section in order.
  const first = stable[0] ?? sections[0]?.entries[0];
  if (!first) return null;

  return (
    <CollapsibleNavGroup
      label={label}
      Icon={Icon}
      defaultHref={first.item.href()}
      stage={stage}
      activeStage={visible.find((entry) => entry.item.active)?.item.stage}
    >
      {stable.map((entry) => (
        <CollapsibleNavItem
          key={entry.item.url}
          item={entry.item}
          label={entry.label}
        />
      ))}
      {sections.map(({ sectionStage, entries }) => (
        <ReleaseStageNavSection key={sectionStage} stage={sectionStage}>
          {entries.map((entry) => (
            <CollapsibleNavItem
              key={entry.item.url}
              item={entry.item}
              label={entry.label}
              stageSection={sectionStage}
            />
          ))}
        </ReleaseStageNavSection>
      ))}
    </CollapsibleNavGroup>
  );
}

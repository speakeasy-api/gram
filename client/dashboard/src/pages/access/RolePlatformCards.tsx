import { Badge } from "@/components/ui/Badge";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import { ChevronDown, X } from "lucide-react";
import type { JSX } from "react";

import { computeRuleLabel } from "./roleDialogState";
import {
  type PlatformArea,
  type PlatformLevel,
  platformAllows,
  platformAreaBlockable,
  platformAreaDenied,
  platformBlock,
  platformLevel,
  setPlatformAllow,
  setPlatformAreaDenied,
  setPlatformLevel,
} from "./roleMatrixModel";
import {
  DeniedBadge,
  type DenyTarget,
  DropdownChip,
  EditorSection,
  type Grants,
  type ProjectRef,
} from "./roleMatrixParts";
import { isProjectScopedResourceType } from "./types";

const LEVEL_NAME: Record<PlatformLevel, string> = {
  none: "No access",
  view: "View",
  manage: "Manage",
};

/** Whether the area is granted anything at all. */
function areaActive(grants: Grants, area: PlatformArea): boolean {
  return (
    platformLevel(grants, area) !== "none" ||
    area.extras.some((scope) => platformAllows(grants, scope.slug))
  );
}

/**
 * Everything a role can see or change outside MCP servers: one card per area,
 * one level each, with the area's single permissions as checkboxes under it.
 */
export function RolePlatformCards({
  grants,
  onChange,
  areas,
  projects,
  markAgentIneligible,
  onEditRule,
  onRemoveRule,
  onDeny,
}: {
  grants: Grants;
  onChange: (change: (prev: Grants) => Grants) => void;
  areas: PlatformArea[];
  projects: ProjectRef[];
  markAgentIneligible: boolean;
  onEditRule: (scope: Scope, ruleIndex: number) => void;
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
  onDeny: (target: DenyTarget) => void;
}): JSX.Element {
  const granted = areas.filter((area) => areaActive(grants, area)).length;
  return (
    <EditorSection
      title="Platform access"
      description="Everything else the role can see or change. One level per area; Manage includes View."
      count={`${granted} ${granted === 1 ? "area" : "areas"} granted`}
    >
      <div className="@container">
        <div className="grid grid-cols-1 gap-3 @2xl:grid-cols-2 @5xl:grid-cols-3">
          {areas.map((area) => (
            <AreaCard
              key={area.category}
              area={area}
              grants={grants}
              onChange={onChange}
              projects={projects}
              markAgentIneligible={markAgentIneligible}
              onEditRule={onEditRule}
              onRemoveRule={onRemoveRule}
              onDeny={onDeny}
            />
          ))}
        </div>
      </div>
    </EditorSection>
  );
}

function AreaCard({
  area,
  grants,
  onChange,
  projects,
  markAgentIneligible,
  onEditRule,
  onRemoveRule,
  onDeny,
}: {
  area: PlatformArea;
  grants: Grants;
  onChange: (change: (prev: Grants) => Grants) => void;
  projects: ProjectRef[];
  markAgentIneligible: boolean;
  onEditRule: (scope: Scope, ruleIndex: number) => void;
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
  onDeny: (target: DenyTarget) => void;
}): JSX.Element {
  const level = platformLevel(grants, area);
  const levelDef = level === "none" ? undefined : area[level];
  const denied = platformAreaDenied(grants, area);
  const active = areaActive(grants, area);
  const hasLevels = !!(area.view || area.manage);
  const grantedDefs = [
    levelDef,
    ...area.extras.filter((scope) => platformAllows(grants, scope.slug)),
  ];
  const sensitive = grantedDefs.some((def) => def?.sensitive);
  const ineligible =
    markAgentIneligible &&
    [area.view, area.manage, ...area.extras].some(
      (def) => def?.agentEligible === false,
    );
  const where = isProjectScopedResourceType(
    area.resourceType as Parameters<typeof isProjectScopedResourceType>[0],
  )
    ? "all projects"
    : "the whole organization";

  const actions: Action[] = [];
  const narrowIndex = levelDef
    ? (grants[levelDef.slug]?.rules ?? []).findIndex(
        (rule) => rule.effect === "allow",
      )
    : -1;
  if (
    levelDef &&
    narrowIndex >= 0 &&
    !denied &&
    isProjectScopedResourceType(
      area.resourceType as Parameters<typeof isProjectScopedResourceType>[0],
    )
  ) {
    actions.push({
      label: "Narrow…",
      description: "Pick which projects this level covers.",
      onClick: () => onEditRule(levelDef.slug as Scope, narrowIndex),
    });
  }
  if (denied) {
    actions.push({
      label: "Remove deny",
      description: "Back to not granted by this role.",
      destructive: true,
      separatorBefore: actions.length > 0,
      onClick: () =>
        onChange((prev) => setPlatformAreaDenied(prev, area, false)),
    });
  } else {
    const blockable = platformAreaBlockable(area);
    actions.push({
      label: "Deny explicitly…",
      description: blockable
        ? "Rarely needed. Wins over every grant, including direct ones."
        : `${area.label} cannot be denied.`,
      destructive: true,
      disabled: !blockable,
      separatorBefore: actions.length > 0,
      onClick: () =>
        onDeny({
          what: area.label,
          where,
          apply: (prev) => setPlatformAreaDenied(prev, area, true),
        }),
    });
  }

  return (
    <div
      className={cn(
        "border-border bg-card flex min-w-0 flex-col gap-3 border p-4",
        denied && "border-destructive",
      )}
    >
      <div>
        <div className="flex items-center gap-2">
          <span className="min-w-0 truncate font-medium">{area.label}</span>
          {sensitive && (
            <Badge variant="warning" size="sm">
              Sensitive
            </Badge>
          )}
          {ineligible && (
            <Badge variant="neutral" size="sm">
              Not available to agents
            </Badge>
          )}
          <span className="ml-auto shrink-0">
            <MoreActions
              actions={actions}
              triggerAriaLabel={`More for ${area.label}`}
            />
          </span>
        </div>
        {area.description && (
          <p className="text-muted-foreground text-sm">{area.description}</p>
        )}
      </div>

      {denied && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <DeniedBadge />
          <span className="text-muted-foreground">on {where}</span>
        </div>
      )}

      {hasLevels && (
        <LevelMenu
          area={area}
          level={level}
          disabled={denied}
          onChange={(next) =>
            onChange((prev) => setPlatformLevel(prev, area, next))
          }
        />
      )}

      {area.extras.length > 0 && (
        <div className="flex flex-col gap-2">
          {area.extras.map((scope) => {
            const blocked = platformBlock(grants, scope.slug) === "all";
            const checked = platformAllows(grants, scope.slug);
            return (
              <label
                key={scope.slug}
                className={cn(
                  "flex items-center gap-2 text-sm",
                  blocked ? "cursor-default opacity-60" : "cursor-pointer",
                )}
              >
                <Checkbox
                  aria-label={scope.label}
                  checked={checked}
                  // One the deny cannot cover stays editable under it.
                  disabled={blocked}
                  onCheckedChange={(next) =>
                    onChange((prev) =>
                      setPlatformAllow(
                        prev,
                        scope.slug as Scope,
                        next === true,
                      ),
                    )
                  }
                />
                <SimpleTooltip tooltip={scope.description}>
                  <span>{scope.label}</span>
                </SimpleTooltip>
                {blocked && !denied && <DeniedBadge />}
              </label>
            );
          })}
        </div>
      )}

      {active && hasLevels && (
        <AppliesTo
          grants={grants}
          defs={[area.view, area.manage]}
          resourceType={area.resourceType}
          projects={projects}
          onEditRule={onEditRule}
          onRemoveRule={onRemoveRule}
        />
      )}
      {area.extras
        .filter((scope) => grants[scope.slug])
        .map((scope) => (
          <AppliesTo
            key={scope.slug}
            grants={grants}
            defs={[scope]}
            permissionLabel={scope.label}
            // An area of one permission needs no name beside "Applies to".
            named={hasLevels || area.extras.length > 1}
            resourceType={scope.resourceType}
            projects={projects}
            onEditRule={onEditRule}
            onRemoveRule={onRemoveRule}
          />
        ))}
    </div>
  );
}

/** The card's one level: No access, View or Manage. */
function LevelMenu({
  area,
  level,
  disabled,
  onChange,
}: {
  area: PlatformArea;
  level: PlatformLevel;
  disabled: boolean;
  onChange: (level: PlatformLevel) => void;
}): JSX.Element {
  const options: {
    level: PlatformLevel;
    hint: string;
    def?: ScopeDefinition;
  }[] = [
    { level: "none", hint: "Not granted by this role." },
    ...(["view", "manage"] as const).flatMap((key) => {
      const def = area[key];
      return def ? [{ level: key, hint: def.description, def }] : [];
    }),
  ];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <button
          type="button"
          aria-label={`${area.label} access level`}
          className={cn(
            "border-border bg-card flex h-9 w-full items-center justify-between gap-2 border px-3 text-left text-sm",
            disabled
              ? "cursor-default opacity-50"
              : "hover:border-foreground/40 cursor-pointer",
            level === "none" && "text-muted-foreground",
          )}
        >
          <span className="truncate font-medium">{LEVEL_NAME[level]}</span>
          <ChevronDown aria-hidden="true" className="size-4 shrink-0" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80 max-w-[90vw]">
        <DropdownMenuRadioGroup
          value={level}
          onValueChange={(next) => onChange(next as PlatformLevel)}
        >
          {options.map((option) => (
            <DropdownMenuRadioItem
              key={option.level}
              value={option.level}
              aria-label={LEVEL_NAME[option.level]}
              className="items-start"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  {LEVEL_NAME[option.level]}
                  {option.def?.sensitive && (
                    <Badge variant="warning" size="sm">
                      Sensitive
                    </Badge>
                  )}
                </div>
                {option.hint && (
                  <div className="text-muted-foreground text-xs font-normal">
                    {option.hint}
                  </div>
                )}
              </div>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Where a platform permission applies: one chip per rule, which opens the
 * rule editor to narrow it to projects. A block on some projects carries its
 * own remove button, since a card's deny only denies everywhere.
 */
function AppliesTo({
  permissionLabel,
  named = false,
  grants,
  defs,
  resourceType,
  projects,
  onEditRule,
  onRemoveRule,
}: {
  permissionLabel?: string;
  /** Say which permission the line is for, when the card holds several. */
  named?: boolean;
  grants: Grants;
  defs: (ScopeDefinition | undefined)[];
  resourceType: ScopeDefinition["resourceType"];
  projects: ProjectRef[];
  onEditRule: (scope: Scope, ruleIndex: number) => void;
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
}): JSX.Element | null {
  const type = resourceType as Parameters<typeof computeRuleLabel>[1];
  const links = defs.flatMap((def, position) => {
    if (!def) return [];
    return (grants[def.slug]?.rules ?? []).map((rule, index) => ({
      key: `${def.slug}:${rule.id}`,
      scope: def.slug as Scope,
      index,
      blocked: rule.effect === "deny",
      level: permissionLabel ?? (position === 0 ? "View" : "Manage"),
      label: computeRuleLabel(rule.selectors, type, projects),
      everywhere: rule.selectors === null,
    }));
  });
  // A block everywhere is the card's deny; listing it again says nothing.
  const shown = links.filter((link) => !(link.blocked && link.everywhere));
  if (shown.length === 0) return null;
  const lead = (
    <span className="text-muted-foreground">
      {named && permissionLabel
        ? `${permissionLabel} applies to`
        : "Applies to"}
    </span>
  );
  if (!isProjectScopedResourceType(type)) {
    return (
      <div className="flex flex-wrap items-center gap-2 text-sm">
        {lead}
        <DropdownChip
          label="Applies to the whole organization"
          disabledReason="Organization permissions cannot be narrowed"
        >
          Whole organization
        </DropdownChip>
      </div>
    );
  }
  // The same place granted at both levels reads once; different places say
  // which level each is for.
  const allows = shown.filter((link) => !link.blocked);
  const levelled = new Set(allows.map((link) => link.label)).size > 1;
  const seen = new Set<string>();
  const unique = shown.filter((link) => {
    const id = link.blocked || levelled ? link.key : link.label;
    if (seen.has(id)) return false;
    seen.add(id);
    return true;
  });
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      {lead}
      {unique.map((link) => (
        <span
          key={link.key}
          className="inline-flex max-w-full items-center gap-1"
        >
          <DropdownChip
            label={`Change where ${link.blocked ? "the block" : link.level} applies`}
            tone={link.blocked ? "destructive" : "default"}
            onClick={() => onEditRule(link.scope, link.index)}
          >
            {link.blocked ? "Blocked: " : levelled ? `${link.level}: ` : ""}
            {link.label}
          </DropdownChip>
          {link.blocked && (
            <button
              type="button"
              onClick={() => onRemoveRule(link.scope, link.index)}
              aria-label={`Remove block on ${link.label}`}
              className="text-muted-foreground hover:text-foreground cursor-pointer p-1"
            >
              <X aria-hidden="true" className="size-3.5" />
            </button>
          )}
        </span>
      ))}
    </div>
  );
}

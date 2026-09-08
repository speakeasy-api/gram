import { Button as LocalButton } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useOrganization } from "@/contexts/Auth";
import type { DirectoryMapping } from "@gram/client/models/components/directorymapping.js";
import { useListScopes } from "@gram/client/react-query/listScopes.js";
import { useUpsertDirectoryMappingMutation } from "@gram/client/react-query/upsertDirectoryMapping.js";
import { invalidateAllDirectoryMappings } from "@gram/client/react-query/directoryMappings.js";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Ban,
  Check,
  ChevronDown,
  ChevronRight,
  Loader2,
  Plus,
  X,
} from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { useMemo, useState } from "react";
import { GrantRuleDrawerContent } from "./GrantRuleDrawerContent";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import type { ActivePanel, ResourceType, RoleGrant, ScopeRule } from "./types";
import {
  isProjectSelectableResourceType,
  isUnrestrictedResourceType,
  unrestrictedResourceLabel,
} from "./types";
import {
  effectiveGrantCount,
  grantKeysString,
  computeRuleLabel,
  computeRuleTooltip,
} from "./roleDialogState";
import {
  applyRemoveRule,
  grantsFromSdkGrants,
  sdkGrantsFromForm,
} from "./roleGrantTransform";
import {
  mappingKindLabel,
  mappingLabel,
  type MappingTargetOption,
} from "./mappingHelpers";

function getAllowLevel(
  rules: ScopeRule[],
): "all" | "project" | "server" | "tool" | "annotation" | null {
  const allows = rules.filter((r) => r.effect === "allow");
  if (allows.length === 0) return null;
  if (allows.some((r) => r.selectors === null)) return "all";
  const allSels = allows.flatMap((r) => r.selectors ?? []);
  if (allSels.some((s) => s.projectId)) return "project";
  if (allSels.some((s) => s.disposition)) return "annotation";
  if (allSels.some((s) => s.tool)) return "tool";
  return "server";
}

function getDenyPanels(
  allowLevel: string | null,
  projectSelectable = false,
): ActivePanel[] {
  if (projectSelectable) {
    return allowLevel === "all" ? ["servers"] : [];
  }

  switch (allowLevel) {
    case "all":
      return ["projects", "servers", "tools"];
    case "project":
      return ["servers", "tools"];
    case "server":
      return ["tools"];
    case null:
    default:
      return [];
  }
}

interface MappingDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editingMapping?: DirectoryMapping | null;
  availableTargets: MappingTargetOption[];
}

export function MappingDialog({
  open,
  onOpenChange,
  editingMapping,
  availableTargets,
}: MappingDialogProps): JSX.Element {
  const isEditing = !!editingMapping;
  const [principalUrn, setPrincipalUrn] = useState("");
  const [grants, setGrants] = useState<Record<string, RoleGrant>>({});
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
  const [initialGrantKeys, setInitialGrantKeys] = useState("");
  const [initialized, setInitialized] = useState(false);
  type DialogStep = "form" | "rule-editor";
  const [dialogStep, setDialogStep] = useState<DialogStep>("form");
  const [editingScopeSlug, setEditingScopeSlug] = useState<Scope | null>(null);
  const [editingRuleIndex, setEditingRuleIndex] = useState(-1);
  const [draftRule, setDraftRule] = useState<ScopeRule | null>(null);

  const queryClient = useQueryClient();
  const organization = useOrganization();
  const { data: scopesData } = useListScopes();
  const scopeDefinitions = scopesData?.scopes;
  const userVisibleScopeDefinitions = useMemo(
    () =>
      (scopeDefinitions ?? []).filter(
        (scope) => scope.visibility === "user_visible",
      ),
    [scopeDefinitions],
  );
  const projectList = useMemo(
    () => organization.projects.map((p) => ({ id: p.id, name: p.name })),
    [organization.projects],
  );
  const scopeGroups = useMemo(() => {
    const groupOrder: {
      label: string;
      resourceType: ResourceType;
      description: string;
    }[] = [
      {
        label: "Organization",
        resourceType: "org",
        description: "Organization metadata, members, and access settings.",
      },
      {
        label: "Build & Deploy",
        resourceType: "project",
        description: "Projects and their related resources.",
      },
      {
        label: "Environments",
        resourceType: "environment",
        description: "Environments and their entries within projects.",
      },
      {
        label: "Skills",
        resourceType: "skill",
        description: "Skills available within projects.",
      },
      {
        label: "MCP Servers",
        resourceType: "mcp",
        description: "MCP server configuration and connections.",
      },
      {
        label: "Agent Sessions",
        resourceType: "chat",
        description: "Access to members' agent session transcripts.",
      },
      {
        label: "Agents",
        resourceType: "agent",
        description: "Agents available within the organization.",
      },
    ];
    return groupOrder.map((g) => ({
      ...g,
      scopes: userVisibleScopeDefinitions.filter(
        (s) => s.resourceType === g.resourceType,
      ),
    }));
  }, [userVisibleScopeDefinitions]);

  if (editingMapping && !initialized && scopesData) {
    const mappedGrants = grantsFromSdkGrants(
      editingMapping.grants,
      scopesData.scopes,
    );
    setExpandedGroups(new Set());
    setGrants(mappedGrants);
    setPrincipalUrn(editingMapping.principalUrn);
    setInitialGrantKeys(grantKeysString(mappedGrants));
    setInitialized(true);
  }
  if (!editingMapping && initialized) {
    setInitialized(false);
  }

  const upsertMapping = useUpsertDirectoryMappingMutation({
    onSuccess: async () => {
      await invalidateAllDirectoryMappings(queryClient);
      handleClose();
    },
  });

  const visibleScopeSlugs = useMemo(
    () =>
      new Set<Scope>(
        scopeGroups.flatMap((group) =>
          group.scopes.map((s) => s.slug as Scope),
        ),
      ),
    [scopeGroups],
  );
  const visibleGrants = useMemo(
    () =>
      Object.fromEntries(
        Object.entries(grants).filter(([scope]) =>
          visibleScopeSlugs.has(scope as Scope),
        ),
      ),
    [grants, visibleScopeSlugs],
  );
  const grantCount = effectiveGrantCount(visibleGrants);
  const selectedTarget = availableTargets.find(
    (target) => target.principalUrn === principalUrn,
  );
  const saveDisabled =
    !scopeDefinitions ||
    upsertMapping.isPending ||
    !principalUrn ||
    grantCount === 0 ||
    (isEditing && grantKeysString(grants) === initialGrantKeys);

  const toggleScope = (scope: Scope) => {
    setGrants((prev) => {
      const next = { ...prev };
      if (next[scope]) {
        delete next[scope];
      } else {
        next[scope] = {
          scope,
          rules: [
            { id: crypto.randomUUID(), effect: "allow", selectors: null },
          ],
        };
      }
      return next;
    });
  };

  const openRuleEditor = (scopeSlug: Scope, ruleIndex: number) => {
    setEditingScopeSlug(scopeSlug);
    setEditingRuleIndex(ruleIndex);

    const grant = grants[scopeSlug];
    if (ruleIndex >= 0 && grant?.rules[ruleIndex]) {
      setDraftRule({ ...grant.rules[ruleIndex]! });
    } else {
      const existingDenyIdx = grant?.rules.findIndex(
        (r) => r.effect === "deny",
      );
      if (existingDenyIdx !== undefined && existingDenyIdx >= 0) {
        setEditingRuleIndex(existingDenyIdx);
        setDraftRule({ ...grant!.rules[existingDenyIdx]! });
      } else {
        setDraftRule({
          id: crypto.randomUUID(),
          effect: "deny",
          selectors: [],
        });
      }
    }
    setDialogStep("rule-editor");
  };

  const saveAndCloseRuleEditor = () => {
    if (draftRule && editingScopeSlug) {
      const hasContent =
        draftRule.selectors === null || draftRule.selectors.length > 0;
      if (hasContent) {
        setGrants((prev) => {
          const grant = prev[editingScopeSlug] ?? {
            scope: editingScopeSlug,
            rules: [],
          };
          let rules = [...grant.rules];
          if (editingRuleIndex >= 0 && editingRuleIndex < rules.length) {
            const originalRule = grant.rules[editingRuleIndex];
            rules[editingRuleIndex] = draftRule;
            const toSortedJSON = (s: Selector[] | null | undefined) =>
              JSON.stringify(
                [...(s ?? [])].sort((a, b) =>
                  JSON.stringify(a).localeCompare(JSON.stringify(b)),
                ),
              );
            if (
              draftRule.effect === "allow" &&
              toSortedJSON(originalRule?.selectors) !==
                toSortedJSON(draftRule.selectors)
            ) {
              rules = rules.filter((r) => r.effect !== "deny");
            }
          } else if (draftRule.effect === "deny") {
            rules = rules.filter((r) => r.effect !== "deny");
            rules.push(draftRule);
          } else {
            rules.push(draftRule);
          }
          return {
            ...prev,
            [editingScopeSlug]: { scope: editingScopeSlug, rules },
          };
        });
      }
    }
    setDialogStep("form");
    setTimeout(() => {
      setEditingScopeSlug(null);
      setEditingRuleIndex(-1);
      setDraftRule(null);
    }, 300);
  };

  const removeRule = (scopeSlug: string, ruleIndex: number) => {
    setGrants((prev) => {
      const grant = prev[scopeSlug];
      if (!grant) return prev;
      const result = applyRemoveRule(grant, ruleIndex);
      const next = { ...prev };
      if (result === null) {
        delete next[scopeSlug];
      } else {
        next[scopeSlug] = result;
      }
      return next;
    });
  };

  const toggleGroup = (label: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(label)) next.delete(label);
      else next.add(label);
      return next;
    });
  };

  const toggleGroupCheckbox = (label: string) => {
    const group = scopeGroups.find((g) => g.label === label);
    if (!group) return;
    setGrants((prev) => {
      const allSelected = group.scopes.every((s) => prev[s.slug]);
      const next = { ...prev };
      for (const scope of group.scopes) {
        if (allSelected) {
          delete next[scope.slug];
        } else if (!next[scope.slug]) {
          next[scope.slug] = {
            scope: scope.slug,
            rules: [
              { id: crypto.randomUUID(), effect: "allow", selectors: null },
            ],
          };
        }
      }
      return next;
    });
  };

  const handleSubmit = () => {
    if (!scopeDefinitions || !principalUrn) return;
    upsertMapping.mutate({
      request: {
        upsertDirectoryMappingForm: {
          principalUrn,
          grants: sdkGrantsFromForm(grants, scopeDefinitions),
        },
      },
    });
  };

  const handleClose = () => {
    setPrincipalUrn("");
    setGrants({});
    setExpandedGroups(new Set());
    setInitialGrantKeys("");
    setInitialized(false);
    setDialogStep("form");
    setEditingScopeSlug(null);
    setEditingRuleIndex(-1);
    setDraftRule(null);
    onOpenChange(false);
  };

  const editingScopeDef = editingScopeSlug
    ? scopeGroups
        .flatMap((g) => g.scopes)
        .find((s) => s.slug === editingScopeSlug)
    : null;
  const editingGrantRules = editingScopeSlug
    ? (grants[editingScopeSlug]?.rules ?? [])
    : [];
  const allowLevel = getAllowLevel(editingGrantRules);
  const denyAllowedPanels = getDenyPanels(
    allowLevel,
    editingScopeDef
      ? isProjectSelectableResourceType(editingScopeDef.resourceType)
      : false,
  );
  const stepOffset =
    dialogStep === "form" ? "translate-x-0" : "-translate-x-full";

  return (
    <Sheet open={open} onOpenChange={handleClose}>
      <SheetContent
        side="right"
        className={cn(
          "flex w-full flex-col gap-1 overflow-hidden sm:max-w-2xl",
        )}
      >
        <SheetHeader className="border-border border-b">
          <SheetTitle>
            {dialogStep === "rule-editor" && editingScopeDef ? (
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={saveAndCloseRuleEditor}
                  className="text-muted-foreground hover:text-foreground -ml-1 p-1 transition-colors"
                >
                  <ArrowLeft className="h-4 w-4" />
                </button>
                <span>
                  {editingRuleIndex >= 0 ? "Edit" : "Create"}{" "}
                  {draftRule?.effect === "allow" ? "allow" : "exception"} rule
                </span>
              </div>
            ) : isEditing ? (
              "Edit mapping"
            ) : (
              "Add mapping"
            )}
          </SheetTitle>
          {dialogStep === "rule-editor" && draftRule ? (
            <SheetDescription className="text-muted-foreground mr-5 ml-7 line-clamp-2 text-xs">
              {draftRule.effect === "allow"
                ? "Choose which resources this mapping can access. Start broad — you can add exceptions later to restrict specific items."
                : "Exclude specific resources that the allow rule would otherwise permit."}
            </SheetDescription>
          ) : (
            <SheetDescription className="text-muted-foreground text-sm">
              Assign extra permissions to a directory group or identity-provider
              attribute. These grants are supplemental to SCIM role assignment.
            </SheetDescription>
          )}
        </SheetHeader>

        <div className="relative flex-1 overflow-hidden">
          <div
            className={cn(
              "flex h-full transition-transform duration-300 ease-in-out",
              stepOffset,
            )}
          >
            <div className="w-full shrink-0 space-y-4 overflow-y-auto px-4 pt-3">
              {isEditing && editingMapping ? (
                <div>
                  <Text variant="body" className="mb-1 font-medium">
                    Target
                  </Text>
                  <Text variant="body">{mappingLabel(editingMapping)}</Text>
                  <Text muted small>
                    {mappingKindLabel(editingMapping.kind)}
                    {editingMapping.memberCount === 1
                      ? " · 1 member"
                      : ` · ${String(editingMapping.memberCount)} members`}
                  </Text>
                </div>
              ) : (
                <label className="block space-y-1.5">
                  <Text variant="body" className="font-medium">
                    Directory target
                  </Text>
                  <select
                    aria-label="Directory target"
                    className="border-input bg-background h-9 w-full border px-3 text-sm"
                    value={principalUrn}
                    onChange={(event) => setPrincipalUrn(event.target.value)}
                  >
                    <option value="">Choose a group or attribute</option>
                    {availableTargets.map((target) => (
                      <option
                        key={target.principalUrn}
                        value={target.principalUrn}
                      >
                        {mappingKindLabel(target.kind)} · {target.label}
                      </option>
                    ))}
                  </select>
                  {selectedTarget ? (
                    <Text muted small>
                      {selectedTarget.memberCount === 1
                        ? "1 matching member"
                        : `${String(selectedTarget.memberCount)} matching members`}
                    </Text>
                  ) : null}
                </label>
              )}

              <div className="border-border border-t pt-4">
                <div className="flex items-center gap-1">
                  <Text variant="body" className="font-medium">
                    Permissions
                  </Text>
                  <Text variant="body" className="text-muted-foreground ml-1">
                    ({grantCount} selected)
                  </Text>
                </div>

                <div className="mt-3 space-y-3">
                  {scopeGroups.map((group) => {
                    const selectedInGroup = group.scopes.filter(
                      (s) => grants[s.slug],
                    ).length;
                    const isExpanded = expandedGroups.has(group.label);
                    const allSelected =
                      group.scopes.length > 0 &&
                      group.scopes.every((s) => grants[s.slug]);
                    const someSelected = selectedInGroup > 0 && !allSelected;

                    return (
                      <div key={group.label} className="border-border border">
                        <div
                          role="button"
                          tabIndex={0}
                          onClick={() => toggleGroup(group.label)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault();
                              toggleGroup(group.label);
                            }
                          }}
                          className="hover:bg-muted/50 flex w-full cursor-pointer items-start justify-between gap-2 px-3 py-2"
                        >
                          <div className="flex items-start gap-2">
                            <Checkbox
                              checked={
                                allSelected
                                  ? true
                                  : someSelected
                                    ? "indeterminate"
                                    : false
                              }
                              onClick={(e) => {
                                e.stopPropagation();
                                toggleGroupCheckbox(group.label);
                              }}
                              className="mt-0.5 cursor-pointer"
                            />
                            <div className="min-w-0">
                              <div className="flex items-center gap-2">
                                <Text
                                  variant="body"
                                  className="text-sm font-medium"
                                >
                                  {group.label}
                                </Text>
                                <Text
                                  variant="body"
                                  className="text-muted-foreground text-sm"
                                >
                                  ({selectedInGroup}/{group.scopes.length})
                                </Text>
                              </div>
                              {!isExpanded && (
                                <Text muted small className="mt-0.5 text-xs">
                                  {group.description}
                                </Text>
                              )}
                            </div>
                          </div>
                          <ChevronRight
                            className={cn(
                              "text-muted-foreground mt-0.5 h-3.5 w-3.5 shrink-0 transition-transform",
                              isExpanded && "rotate-90",
                            )}
                          />
                        </div>

                        {isExpanded ? (
                          <div className="border-border bg-muted/40 border-t">
                            {group.scopes.map((scopeDef) => {
                              const grant = grants[scopeDef.slug];
                              const isChecked = !!grant;
                              const isConfigurable =
                                !isUnrestrictedResourceType(
                                  scopeDef.resourceType,
                                );

                              return (
                                <div key={scopeDef.slug}>
                                  <div className="hover:bg-muted/50 flex items-start gap-3 px-3 py-2.5">
                                    <label className="flex min-w-0 flex-1 cursor-pointer items-start gap-3">
                                      <Checkbox
                                        checked={isChecked}
                                        onCheckedChange={() =>
                                          toggleScope(scopeDef.slug)
                                        }
                                        className="bg-background mt-0.5"
                                      />
                                      <div className="min-w-0 flex-1">
                                        <Text
                                          variant="body"
                                          className="font-mono text-sm font-medium"
                                        >
                                          {scopeDef.slug}
                                        </Text>
                                        <Text
                                          variant="body"
                                          className="text-muted-foreground text-xs"
                                        >
                                          {scopeDef.description}
                                        </Text>
                                      </div>
                                    </label>
                                    {isChecked && !isConfigurable ? (
                                      <span className="border-input text-muted-foreground inline-flex h-7 shrink-0 items-center border bg-transparent px-2 py-1 text-xs">
                                        {unrestrictedResourceLabel(
                                          scopeDef.resourceType,
                                        )}
                                      </span>
                                    ) : null}
                                  </div>

                                  {isChecked && isConfigurable && grant ? (
                                    <div className="mr-3 ml-8 flex flex-wrap items-center gap-1.5 pb-3">
                                      {grant.rules.map((rule, ruleIdx) => (
                                        <RuleChip
                                          key={rule.id}
                                          rule={rule}
                                          label={computeRuleLabel(
                                            rule.selectors,
                                            scopeDef.resourceType,
                                            projectList,
                                          )}
                                          tooltip={computeRuleTooltip(
                                            rule.effect,
                                            rule.selectors,
                                            scopeDef.resourceType,
                                            projectList,
                                          )}
                                          onClick={() =>
                                            openRuleEditor(
                                              scopeDef.slug,
                                              ruleIdx,
                                            )
                                          }
                                          onRemove={() =>
                                            removeRule(scopeDef.slug, ruleIdx)
                                          }
                                        />
                                      ))}
                                      {!grant.rules.some(
                                        (r) => r.effect === "deny",
                                      ) &&
                                      getDenyPanels(
                                        getAllowLevel(grant.rules),
                                        isProjectSelectableResourceType(
                                          scopeDef.resourceType,
                                        ),
                                      ).length > 0 ? (
                                        <LocalButton
                                          type="button"
                                          variant="tertiary"
                                          size="xs"
                                          className="text-muted-foreground text-xs"
                                          onClick={() =>
                                            openRuleEditor(scopeDef.slug, -1)
                                          }
                                        >
                                          <Plus className="h-3 w-3" />
                                          Except…
                                        </LocalButton>
                                      ) : null}
                                    </div>
                                  ) : null}
                                </div>
                              );
                            })}
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>

            <div className="flex w-full shrink-0 flex-col overflow-hidden">
              {editingScopeDef && draftRule ? (
                <GrantRuleDrawerContent
                  resourceType={editingScopeDef.resourceType}
                  scope={editingScopeSlug!}
                  selectors={draftRule.selectors}
                  onChangeSelectors={(sels) =>
                    setDraftRule((prev) =>
                      prev ? { ...prev, selectors: sels } : null,
                    )
                  }
                  annotations={draftRule.annotations}
                  onChangeAnnotations={(annotations) =>
                    setDraftRule((prev) =>
                      prev ? { ...prev, annotations } : null,
                    )
                  }
                  isDeny={draftRule.effect === "deny"}
                  allowedPanels={
                    draftRule.effect === "deny" ? denyAllowedPanels : undefined
                  }
                  allowSelectors={
                    draftRule.effect === "deny" && editingScopeSlug
                      ? (grants[editingScopeSlug]?.rules.find(
                          (r) => r.effect === "allow",
                        )?.selectors ?? null)
                      : undefined
                  }
                />
              ) : null}
            </div>
          </div>
        </div>

        {dialogStep === "rule-editor" ? (
          <SheetFooter className="border-border flex-row justify-end border-t">
            <Button variant="primary" onClick={saveAndCloseRuleEditor}>
              <Button.LeftIcon>
                <Check className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Done</Button.Text>
            </Button>
          </SheetFooter>
        ) : (
          <SheetFooter className="border-border flex-row justify-end border-t">
            <Button variant="secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button onClick={handleSubmit} disabled={saveDisabled}>
              {upsertMapping.isPending ? (
                <Button.LeftIcon>
                  <Loader2 className="h-4 w-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {upsertMapping.isPending
                  ? "Saving…"
                  : isEditing
                    ? "Save Changes"
                    : "Add Mapping"}
              </Button.Text>
            </Button>
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
  );
}

function RuleChip({
  rule,
  label,
  tooltip,
  onClick,
  onRemove,
}: {
  rule: ScopeRule;
  label: string;
  tooltip?: string;
  onClick?: () => void;
  onRemove?: () => void;
}) {
  const isAllow = rule.effect === "allow";
  const isDeny = !isAllow;
  const chip = (
    <span
      className={cn(
        "border-input bg-background inline-flex items-center gap-1 overflow-hidden border px-1 py-1 text-xs",
        isDeny && "border-destructive/30",
      )}
    >
      <button
        type="button"
        onClick={onClick}
        className={cn(
          "hover:bg-accent inline-flex cursor-pointer items-center gap-1 px-2 py-1 transition-colors",
          isDeny
            ? "text-destructive hover:bg-destructive/5"
            : "text-foreground",
        )}
      >
        {isAllow ? (
          <Check className="text-default-success h-3 w-3 shrink-0" />
        ) : (
          <Ban className="h-3 w-3 shrink-0 opacity-70" />
        )}
        <span className="max-w-[160px] truncate">{label}</span>
        <ChevronDown className="text-muted-foreground -mr-0.5 h-3 w-3 shrink-0" />
      </button>
      {onRemove ? (
        <>
          <div
            className={cn(
              "bg-border h-4 w-px shrink-0",
              isDeny && "bg-destructive/20",
            )}
          />
          <button
            type="button"
            onClick={onRemove}
            className={cn(
              "hover:bg-accent inline-flex items-center px-1.5 py-1 transition-colors",
              isDeny
                ? "text-destructive/60 hover:text-destructive hover:bg-destructive/5"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <X className="h-3 w-3" />
          </button>
        </>
      ) : null}
    </span>
  );

  if (!tooltip) return chip;

  return (
    <Tooltip delayDuration={300}>
      <TooltipTrigger asChild>{chip}</TooltipTrigger>
      <TooltipContent side="bottom" className="text-xs whitespace-nowrap">
        {tooltip}
      </TooltipContent>
    </Tooltip>
  );
}

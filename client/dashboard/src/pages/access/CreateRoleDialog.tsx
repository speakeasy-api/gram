import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button as LocalButton } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useOrganization } from "@/contexts/Auth";
import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant as SdkRoleGrant } from "@gram/client/models/components/rolegrant.js";
import { useCreateRoleMutation } from "@gram/client/react-query/createRole.js";
import {
  invalidateAllMembers,
  useMembers,
} from "@gram/client/react-query/members.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import { useListScopes } from "@gram/client/react-query/listScopes.js";
import { useUpdateRoleMutation } from "@gram/client/react-query/updateRole.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  Ban,
  Check,
  ChevronDown,
  ChevronRight,
  Info,
  Loader2,
  Plus,
  X,
} from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useMemo, useState } from "react";
import { GrantRuleDrawerContent } from "./GrantRuleDrawerContent";
import type {
  ActivePanel,
  AnnotationHint,
  PolicyEffect,
  RoleGrant,
  Scope,
  ScopeRule,
} from "./types";
import type { Selector } from "./types";
import { DISPOSITION_TO_ANNOTATION } from "./types";
import {
  isSaveDisabled,
  effectiveGrantCount,
  grantKeysString as grantKeysStringFn,
  computeRuleLabel,
  computeRuleTooltip,
} from "./roleDialogState";

// ─── Helpers ────────────────────────────────────────────────────────────────

/** Human-readable label for a rule chip. */
/** Split a flat selector array into groups by hierarchy level. */
function groupSelectorsByLevel(selectors: Selector[]): Selector[][] {
  const projects: Selector[] = [];
  const servers: Selector[] = [];
  const tools: Selector[] = [];
  const annotations: Selector[] = [];

  for (const s of selectors) {
    if (s.disposition) annotations.push(s);
    else if (s.tool) tools.push(s);
    else if (s.projectId) projects.push(s);
    else servers.push(s);
  }

  const groups: Selector[][] = [];
  if (projects.length) groups.push(projects);
  if (servers.length) groups.push(servers);
  if (tools.length) groups.push(tools);
  if (annotations.length) groups.push(annotations);
  return groups;
}

/** Convert API Role grants to rules-based RoleGrant map. */
function grantsFromRole(role: Role): Record<string, RoleGrant> {
  const result: Record<string, RoleGrant> = {};

  for (const g of role.grants) {
    if (!result[g.scope]) {
      result[g.scope] = { scope: g.scope, rules: [] };
    }

    const effect: PolicyEffect = (g.effect as PolicyEffect) ?? "allow";

    if (!g.selectors || g.selectors.length === 0) {
      // Unrestricted rule
      result[g.scope].rules.push({
        id: crypto.randomUUID(),
        effect,
        selectors: null,
      });
    } else {
      // Split selectors by hierarchy level into separate rules
      const groups = groupSelectorsByLevel(g.selectors as Selector[]);
      for (const sels of groups) {
        const rule: ScopeRule = {
          id: crypto.randomUUID(),
          effect,
          selectors: sels,
        };
        // Detect annotation-based rules and restore UI hints
        if (sels.some((s) => s.disposition)) {
          rule.customTab = "auto-groups";
          rule.annotations = sels
            .filter((s) => s.disposition)
            .map((s) => DISPOSITION_TO_ANNOTATION[s.disposition!])
            .filter((a): a is AnnotationHint => !!a);
        }
        result[g.scope].rules.push(rule);
      }
    }
  }

  return result;
}

/** Determine the broadest allow level from a scope's rules. */
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

/** Map an allow level to the panels available for deny rules — all levels narrower. */
function getDenyPanels(allowLevel: string | null): ActivePanel[] {
  switch (allowLevel) {
    case "all":
      return ["projects", "servers", "tools"];
    case "project":
      return ["servers", "tools"];
    case "server":
      return ["tools"];
    default:
      return []; // tool/annotation — already most specific, no deny possible
  }
}

// ─── Component ──────────────────────────────────────────────────────────────

interface CreateRoleDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editingRole?: Role | null;
  onRoleCreated?: (roleName: string) => void;
}

export function CreateRoleDialog({
  open,
  onOpenChange,
  editingRole,
  onRoleCreated,
}: CreateRoleDialogProps) {
  const isEditing = !!editingRole;
  const isSystemRole = !!editingRole?.isSystem;

  // ─── Form state ───────────────────────────────────────────────
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [grants, setGrants] = useState<Record<string, RoleGrant>>({});
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
  const [selectedMembers, setSelectedMembers] = useState<Set<string>>(
    new Set(),
  );
  const [initialMembers, setInitialMembers] = useState<Set<string>>(new Set());
  const [initialName, setInitialName] = useState("");
  const [initialDescription, setInitialDescription] = useState("");
  const [initialGrantKeys, setInitialGrantKeys] = useState("");
  const [showMembers, setShowMembers] = useState(false);
  const [showPermissions, setShowPermissions] = useState(true);
  const [initialized, setInitialized] = useState(false);

  // ─── Rule editor state ────────────────────────────────────────
  type DialogStep = "form" | "rule-editor";
  const [dialogStep, setDialogStep] = useState<DialogStep>("form");
  const [editingScopeSlug, setEditingScopeSlug] = useState<Scope | null>(null);
  const [editingRuleIndex, setEditingRuleIndex] = useState<number>(-1);
  const [draftRule, setDraftRule] = useState<ScopeRule | null>(null);

  // ─── Hooks ────────────────────────────────────────────────────
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const { data: membersData } = useMembers();
  const members = membersData?.members ?? [];
  const { data: rolesData } = useRoles();
  const roleNameById = new Map(
    (rolesData?.roles ?? []).map((r) => [r.id, r.name]),
  );
  const { data: scopesData } = useListScopes();

  const projectList = useMemo(
    () => organization.projects.map((p) => ({ id: p.id, name: p.name })),
    [organization.projects],
  );

  const scopeGroups = useMemo(() => {
    const scopes = scopesData?.scopes ?? [];
    const groupOrder: { label: string; resourceType: string }[] = [
      { label: "Organization", resourceType: "org" },
      { label: "Build & Deploy", resourceType: "project" },
      { label: "Environments", resourceType: "environment" },
      { label: "MCP Servers", resourceType: "mcp" },
    ];
    return groupOrder.map((g) => ({
      ...g,
      scopes: scopes.filter((s) => s.resourceType === g.resourceType),
    }));
  }, [scopesData]);

  // ─── Initialize when editing ──────────────────────────────────
  if (editingRole && !initialized && scopesData && membersData) {
    setName(editingRole.name);
    setDescription(editingRole.description);
    const roleGrants = grantsFromRole(editingRole);
    const grantedScopes = new Set(Object.keys(roleGrants));
    const autoExpanded = new Set(
      scopeGroups
        .filter((g) => g.scopes.some((s) => grantedScopes.has(s.slug)))
        .map((g) => g.label),
    );
    setExpandedGroups(autoExpanded);
    setGrants(roleGrants);
    setInitialName(editingRole.name);
    setInitialDescription(editingRole.description);
    setInitialGrantKeys(grantKeysStringFn(roleGrants));
    const assignedIds = new Set(
      members.filter((m) => m.roleId === editingRole.id).map((m) => m.id),
    );
    setSelectedMembers(assignedIds);
    setInitialMembers(new Set(assignedIds));
    setInitialized(true);
  }
  if (!editingRole && initialized) {
    setInitialized(false);
  }

  // ─── Mutations ────────────────────────────────────────────────
  const createRole = useCreateRoleMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRoles(queryClient),
        invalidateAllMembers(queryClient),
      ]);
      onRoleCreated?.(name);
      handleClose();
    },
  });

  const updateRole = useUpdateRoleMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRoles(queryClient),
        invalidateAllMembers(queryClient),
      ]);
      handleClose();
    },
  });

  const isMutating = createRole.isPending || updateRole.isPending;
  const grantCount = effectiveGrantCount(grants);

  const saveDisabled = isSaveDisabled({
    isMutating,
    isEditing,
    isSystemRole,
    name,
    description,
    grants,
    selectedMembers,
    initial: {
      name: initialName,
      description: initialDescription,
      grantKeys: initialGrantKeys,
      members: initialMembers,
    },
  });

  // ─── Scope / grant operations ─────────────────────────────────

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
      // Edit existing rule — clone it as draft
      setDraftRule({ ...grant.rules[ruleIndex] });
    } else {
      // New deny rule — or edit existing deny if one already exists
      const existingDenyIdx = grant?.rules.findIndex(
        (r) => r.effect === "deny",
      );
      if (existingDenyIdx !== undefined && existingDenyIdx >= 0) {
        // Edit the existing deny rule instead of creating a new one
        setEditingRuleIndex(existingDenyIdx);
        setDraftRule({ ...grant!.rules[existingDenyIdx] });
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
            // Editing existing rule — replace in place
            rules[editingRuleIndex] = draftRule;
            // Allow changed → clear deny exceptions (they were scoped to old allow)
            if (draftRule.effect === "allow") {
              rules = rules.filter((r) => r.effect !== "deny");
            }
          } else if (draftRule.effect === "deny") {
            // One deny per scope — replace any existing deny
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
      let rules = grant.rules.filter((_, i) => i !== ruleIndex);
      // No allows left → denies are orphaned, clear everything
      if (!rules.some((r) => r.effect === "allow")) {
        rules = [];
      }
      if (rules.length === 0) {
        const next = { ...prev };
        delete next[scopeSlug];
        return next;
      }
      return { ...prev, [scopeSlug]: { ...grant, rules } };
    });
  };

  // ─── Group operations ─────────────────────────────────────────

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

  // ─── Member operations ────────────────────────────────────────

  const toggleMember = (memberId: string) => {
    setSelectedMembers((prev) => {
      const next = new Set(prev);
      if (next.has(memberId)) next.delete(memberId);
      else next.add(memberId);
      return next;
    });
  };

  const toggleAllMembers = () => {
    const selectableMembers = members.filter(
      (m) => !(isEditing && m.roleId === editingRole?.id),
    );
    setSelectedMembers((prev) => {
      const allSelected = selectableMembers.every((m) => prev.has(m.id));
      const next = new Set(prev);
      for (const m of selectableMembers) {
        if (allSelected) next.delete(m.id);
        else next.add(m.id);
      }
      return next;
    });
  };

  // ─── Submit ───────────────────────────────────────────────────

  const handleSubmit = () => {
    const sdkGrants: SdkRoleGrant[] = [];

    for (const grant of Object.values(grants)) {
      const allowSelectors: Selector[] = [];
      const denySelectors: Selector[] = [];
      let hasUnrestrictedAllow = false;

      for (const rule of grant.rules) {
        if (rule.effect === "allow") {
          if (rule.selectors === null) hasUnrestrictedAllow = true;
          else if (rule.selectors.length > 0)
            allowSelectors.push(...rule.selectors);
        } else {
          if (rule.selectors && rule.selectors.length > 0)
            denySelectors.push(...rule.selectors);
        }
      }

      // Skip scopes with no effective allows
      if (!hasUnrestrictedAllow && allowSelectors.length === 0) continue;

      sdkGrants.push({
        scope: grant.scope,
        selectors: hasUnrestrictedAllow ? undefined : allowSelectors,
      });

      if (denySelectors.length > 0) {
        sdkGrants.push({
          scope: grant.scope,
          effect: "deny",
          selectors: denySelectors,
        });
      }
    }

    if (isEditing) {
      updateRole.mutate({
        request: {
          updateRoleForm: {
            id: editingRole.id,
            ...(isSystemRole ? {} : { name, description, grants: sdkGrants }),
            memberIds:
              selectedMembers.size > 0
                ? Array.from(selectedMembers)
                : undefined,
          },
        },
      });
    } else {
      createRole.mutate({
        request: {
          createRoleForm: {
            name,
            description,
            grants: sdkGrants,
            memberIds:
              selectedMembers.size > 0
                ? Array.from(selectedMembers)
                : undefined,
          },
        },
      });
    }
  };

  // ─── Close / reset ────────────────────────────────────────────

  const handleClose = () => {
    setName("");
    setDescription("");
    setGrants({});
    setExpandedGroups(new Set());
    setSelectedMembers(new Set());
    setInitialMembers(new Set());
    setInitialName("");
    setInitialDescription("");
    setInitialGrantKeys("");
    setShowMembers(false);
    setShowPermissions(true);
    setInitialized(false);
    setDialogStep("form");
    setEditingScopeSlug(null);
    setEditingRuleIndex(-1);
    setDraftRule(null);
    onOpenChange(false);
  };

  // ─── Derived for slide panel ──────────────────────────────────

  const editingScopeDef = editingScopeSlug
    ? scopeGroups
        .flatMap((g) => g.scopes)
        .find((s) => s.slug === editingScopeSlug)
    : null;

  // Deny-level constraints: deny must be one level narrower than the broadest allow.
  const editingGrantRules = editingScopeSlug
    ? (grants[editingScopeSlug]?.rules ?? [])
    : [];
  const allowLevel = getAllowLevel(editingGrantRules);
  const denyAllowedPanels = getDenyPanels(allowLevel);
  const stepOffset =
    dialogStep === "form" ? "translate-x-0" : "-translate-x-full";

  // ─── Render ───────────────────────────────────────────────────

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
                  className="text-muted-foreground hover:text-foreground -ml-1 rounded-sm p-1 transition-colors"
                >
                  <ArrowLeft className="h-4 w-4" />
                </button>
                <span>
                  {editingRuleIndex >= 0 ? "Edit" : "Create"}{" "}
                  {draftRule?.effect === "allow" ? "allow" : "deny"} rule
                </span>
              </div>
            ) : isEditing ? (
              "Edit Role"
            ) : (
              "Create Role"
            )}
          </SheetTitle>
          {dialogStep === "rule-editor" && draftRule && (
            <SheetDescription className="text-muted-foreground mr-5 ml-7 line-clamp-2 text-xs">
              {draftRule.effect === "allow"
                ? "Choose which resources this role can access. Start broad — you can add exceptions later to restrict specific items."
                : "Deny access to specific resources that the allow rule would otherwise permit. Use exceptions to carve out items that this role should not access."}
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
            {/* ─── Panel 1: Role form ─── */}
            <div className="w-full shrink-0 space-y-4 overflow-y-auto px-4 pt-3">
              <InputField
                label="Name"
                placeholder="e.g., Project Manager"
                required
                autoFocus
                disabled={editingRole?.isSystem}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />

              <AnyField
                label="Description"
                render={(props) => (
                  <textarea
                    {...props}
                    rows={2}
                    required
                    disabled={editingRole?.isSystem}
                    placeholder="Describe what this role can do..."
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    className="border-input placeholder:text-muted-foreground focus-visible:ring-ring flex w-full resize-none rounded-md border bg-transparent px-3 py-2 text-sm shadow-xs focus-visible:ring-1 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
                  />
                )}
              />

              {/* ─── Permissions ─── */}
              <div className="border-border border-t pt-4">
                <button
                  type="button"
                  onClick={() => setShowPermissions(!showPermissions)}
                  className="flex w-full items-center gap-1 text-left"
                >
                  <ChevronRight
                    className={cn(
                      "h-4 w-4 transition-transform",
                      showPermissions && "rotate-90",
                    )}
                  />
                  <Type variant="body" className="font-medium">
                    Permissions
                  </Type>
                  <Type variant="body" className="text-muted-foreground ml-1">
                    ({grantCount} selected)
                  </Type>
                </button>

                {showPermissions && (
                  <div className="mt-3 space-y-3">
                    {isSystemRole && (
                      <div className="bg-muted/60 text-muted-foreground flex items-center gap-2 rounded-md px-3 py-2 text-xs">
                        <Info className="h-3.5 w-3.5 shrink-0" />
                        System role permissions are managed by Gram and cannot
                        be changed.
                      </div>
                    )}
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
                        <div
                          key={group.label}
                          className="border-border rounded-md border"
                        >
                          {/* Group header */}
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
                            className="hover:bg-muted/50 flex w-full cursor-pointer items-center justify-between rounded-t-md px-3 py-2"
                          >
                            <div className="flex items-center gap-2">
                              <Checkbox
                                disabled={isSystemRole}
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
                                className="cursor-pointer"
                              />
                              <Type
                                variant="body"
                                className="text-sm font-medium"
                              >
                                {group.label}
                              </Type>
                              <Type
                                variant="body"
                                className="text-muted-foreground text-sm"
                              >
                                ({selectedInGroup}/{group.scopes.length})
                              </Type>
                            </div>
                            <ChevronRight
                              className={cn(
                                "text-muted-foreground h-3.5 w-3.5 transition-transform",
                                isExpanded && "rotate-90",
                              )}
                            />
                          </div>

                          {/* Expanded scope rows */}
                          {isExpanded && (
                            <div className="border-border bg-muted/40 border-t">
                              {group.scopes.map((scopeDef) => {
                                const grant = grants[scopeDef.slug];
                                const isChecked = !!grant;
                                const isConfigurable =
                                  scopeDef.resourceType !== "org" &&
                                  scopeDef.resourceType !== "environment";

                                const row = (
                                  <div key={scopeDef.slug}>
                                    {/* Scope checkbox row */}
                                    <div
                                      className={cn(
                                        "hover:bg-muted/50 flex items-start gap-3 px-3 py-2.5",
                                        isSystemRole && "cursor-default",
                                      )}
                                    >
                                      <label
                                        className={cn(
                                          "flex min-w-0 flex-1 items-start gap-3",
                                          isSystemRole
                                            ? "cursor-default"
                                            : "cursor-pointer",
                                        )}
                                      >
                                        <Checkbox
                                          disabled={isSystemRole}
                                          checked={isChecked}
                                          onCheckedChange={() =>
                                            toggleScope(scopeDef.slug)
                                          }
                                          className="bg-background mt-0.5"
                                        />
                                        <div className="min-w-0 flex-1">
                                          <Type
                                            variant="body"
                                            className="font-mono text-sm font-medium"
                                          >
                                            {scopeDef.slug}
                                          </Type>
                                          <Type
                                            variant="body"
                                            className="text-muted-foreground text-xs"
                                          >
                                            {scopeDef.description}
                                          </Type>
                                        </div>
                                      </label>

                                      {/* Static label for org/environment */}
                                      {isChecked && !isConfigurable && (
                                        <span className="border-input text-muted-foreground inline-flex h-7 shrink-0 items-center rounded-md border bg-transparent px-2 py-1 text-xs">
                                          {scopeDef.resourceType ===
                                          "environment"
                                            ? "All in project"
                                            : "All"}
                                        </span>
                                      )}
                                    </div>

                                    {/* Rule chips for configurable scopes */}
                                    {isChecked && isConfigurable && (
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
                                            onClick={
                                              isSystemRole
                                                ? undefined
                                                : () =>
                                                    openRuleEditor(
                                                      scopeDef.slug,
                                                      ruleIdx,
                                                    )
                                            }
                                            onRemove={
                                              isSystemRole
                                                ? undefined
                                                : () =>
                                                    removeRule(
                                                      scopeDef.slug,
                                                      ruleIdx,
                                                    )
                                            }
                                            readOnly={isSystemRole}
                                          />
                                        ))}
                                        {!isSystemRole &&
                                          !grant.rules.some(
                                            (r) => r.effect === "deny",
                                          ) &&
                                          getDenyPanels(
                                            getAllowLevel(grant.rules),
                                          ).length > 0 && (
                                            <LocalButton
                                              type="button"
                                              variant="ghost"
                                              size="inline"
                                              className="text-muted-foreground text-xs"
                                              onClick={() =>
                                                openRuleEditor(
                                                  scopeDef.slug,
                                                  -1,
                                                )
                                              }
                                            >
                                              <Plus className="h-3 w-3" />
                                              Except…
                                            </LocalButton>
                                          )}
                                      </div>
                                    )}
                                  </div>
                                );

                                if (isSystemRole) {
                                  return (
                                    <Tooltip key={scopeDef.slug}>
                                      <TooltipTrigger asChild>
                                        {row}
                                      </TooltipTrigger>
                                      <TooltipContent
                                        side="right"
                                        className="max-w-48"
                                      >
                                        Cannot edit system role permissions
                                      </TooltipContent>
                                    </Tooltip>
                                  );
                                }

                                return row;
                              })}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>

              {/* ─── Assign Members ─── */}
              <div className="border-border border-t pt-4 pb-4">
                <button
                  type="button"
                  onClick={() => setShowMembers(!showMembers)}
                  className="flex w-full items-center gap-1 text-left"
                >
                  <ChevronRight
                    className={cn(
                      "h-4 w-4 transition-transform",
                      showMembers && "rotate-90",
                    )}
                  />
                  <Type variant="body" className="font-medium">
                    Assign Members
                  </Type>
                  <Type variant="body" className="text-muted-foreground ml-1">
                    (optional, {selectedMembers.size} selected)
                  </Type>
                </button>

                {showMembers && (
                  <div className="border-border divide-border mt-3 divide-y rounded-md border">
                    {/* Select-all header */}
                    {(() => {
                      const selectableMembers = members.filter(
                        (m) => !(isEditing && m.roleId === editingRole?.id),
                      );
                      const allSelected =
                        selectableMembers.length > 0 &&
                        selectableMembers.every((m) =>
                          selectedMembers.has(m.id),
                        );
                      const someSelected =
                        !allSelected &&
                        selectableMembers.some((m) =>
                          selectedMembers.has(m.id),
                        );
                      return (
                        <label className="bg-muted/60 flex cursor-pointer items-center gap-3 px-3 py-2">
                          <Checkbox
                            checked={
                              allSelected
                                ? true
                                : someSelected
                                  ? "indeterminate"
                                  : false
                            }
                            onCheckedChange={() => toggleAllMembers()}
                          />
                          <Type
                            variant="body"
                            className="text-muted-foreground text-sm font-medium"
                          >
                            Select all
                          </Type>
                        </label>
                      );
                    })()}
                    {members.map((member) => {
                      const alreadyHasRole =
                        isEditing && member.roleId === editingRole?.id;
                      return (
                        <label
                          key={member.id}
                          className={cn(
                            "hover:bg-muted/50 flex cursor-pointer items-center gap-3 px-3 py-2.5",
                            alreadyHasRole && "cursor-default opacity-50",
                          )}
                        >
                          <Checkbox
                            checked={
                              alreadyHasRole || selectedMembers.has(member.id)
                            }
                            disabled={alreadyHasRole}
                            onCheckedChange={() =>
                              !alreadyHasRole && toggleMember(member.id)
                            }
                          />
                          <Avatar className="h-7 w-7">
                            {member.photoUrl && (
                              <AvatarImage
                                src={member.photoUrl}
                                alt={member.name}
                              />
                            )}
                            <AvatarFallback className="text-xs">
                              {member.name
                                .split(" ")
                                .map((n) => n[0])
                                .join("")
                                .toUpperCase()
                                .slice(0, 2)}
                            </AvatarFallback>
                          </Avatar>
                          <div className="min-w-0 flex-1 space-y-0.5">
                            <div className="flex items-center gap-2">
                              <Type
                                variant="body"
                                className="text-sm font-medium"
                              >
                                {member.name}
                              </Type>
                              {member.roleId &&
                                roleNameById.get(member.roleId) && (
                                  <div className="flex items-center gap-1">
                                    <Badge
                                      variant="outline"
                                      size="sm"
                                      className={cn(
                                        "font-mono text-[10px] uppercase",
                                        selectedMembers.has(member.id) &&
                                          member.roleId !== editingRole?.id &&
                                          name.trim() &&
                                          "line-through opacity-60",
                                      )}
                                    >
                                      {roleNameById.get(member.roleId)}
                                    </Badge>
                                    {selectedMembers.has(member.id) &&
                                      member.roleId !== editingRole?.id &&
                                      name.trim() && (
                                        <>
                                          <ArrowRight className="text-muted-foreground h-3 w-3 shrink-0" />
                                          <Badge
                                            variant="outline"
                                            size="sm"
                                            className="border-primary text-primary font-mono text-[10px] uppercase"
                                          >
                                            {name}
                                          </Badge>
                                        </>
                                      )}
                                  </div>
                                )}
                            </div>
                            <Type
                              variant="body"
                              className="text-muted-foreground text-xs"
                            >
                              {member.email}
                            </Type>
                          </div>
                        </label>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>

            {/* ─── Panel 2: Rule editor (slides in from right) ─── */}
            <div className="flex w-full shrink-0 flex-col overflow-hidden">
              {editingScopeDef && draftRule && (
                <>
                  {/* Resource picker */}
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
                      draftRule.effect === "deny"
                        ? denyAllowedPanels
                        : undefined
                    }
                    allowSelectors={
                      draftRule.effect === "deny" && editingScopeSlug
                        ? (grants[editingScopeSlug]?.rules.find(
                            (r) => r.effect === "allow",
                          )?.selectors ?? null)
                        : undefined
                    }
                  />
                </>
              )}
            </div>
          </div>
        </div>

        {dialogStep === "rule-editor" && (
          <SheetFooter className="border-border flex-row justify-end border-t">
            <Button variant="primary" onClick={saveAndCloseRuleEditor}>
              <Button.LeftIcon>
                <Check className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Done</Button.Text>
            </Button>
          </SheetFooter>
        )}

        {dialogStep === "form" && (
          <SheetFooter className="border-border flex-row justify-end border-t">
            <Button variant="secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button onClick={handleSubmit} disabled={saveDisabled}>
              {isMutating && (
                <Button.LeftIcon>
                  <Loader2 className="h-4 w-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>
                {isMutating
                  ? isEditing
                    ? "Saving\u2026"
                    : "Creating\u2026"
                  : isEditing
                    ? "Save Changes"
                    : "Create Role"}
              </Button.Text>
            </Button>
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
  );
}

// ─── Sub-components ─────────────────────────────────────────────────────────

function RuleChip({
  rule,
  label,
  tooltip,
  onClick,
  onRemove,
  readOnly,
}: {
  rule: ScopeRule;
  label: string;
  tooltip?: string;
  onClick?: () => void;
  onRemove?: () => void;
  readOnly?: boolean;
}) {
  const isAllow = rule.effect === "allow";
  const isDeny = !isAllow;
  const chip = (
    <span
      className={cn(
        "border-input bg-background inline-flex items-center gap-1 overflow-hidden rounded-md border px-1 py-1 text-xs",
        isDeny && "border-destructive/30",
      )}
    >
      <button
        type="button"
        onClick={onClick}
        disabled={readOnly && !onClick}
        className={cn(
          "hover:bg-accent inline-flex items-center gap-1 rounded-md px-2 py-1 transition-colors",
          isDeny
            ? "text-destructive hover:bg-destructive/5"
            : "text-foreground",
          readOnly && "rounded-md",
          !readOnly && onClick && "cursor-pointer",
        )}
      >
        {isAllow ? (
          <Check className="h-3 w-3 shrink-0 text-emerald-600 dark:text-emerald-400" />
        ) : (
          <Ban className="h-3 w-3 shrink-0 opacity-70" />
        )}
        <span className="max-w-[160px] truncate">{label}</span>
        {!readOnly && onClick && (
          <ChevronDown className="text-muted-foreground -mr-0.5 h-3 w-3 shrink-0" />
        )}
      </button>
      {!readOnly && onRemove && (
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
              "hover:bg-accent inline-flex items-center rounded-md px-1.5 py-1 transition-colors",
              isDeny
                ? "text-destructive/60 hover:text-destructive hover:bg-destructive/5"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <X className="h-3 w-3" />
          </button>
        </>
      )}
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

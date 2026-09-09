import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";

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
import type { Role } from "@gram/client/models/components/role.js";
import { useCreateRoleMutation } from "@gram/client/react-query/createRole.js";
import {
  invalidateAllMembers,
  useMembers,
} from "@gram/client/react-query/members.js";
import { invalidateAllRoles } from "@gram/client/react-query/roles.js";
import { useListScopes } from "@gram/client/react-query/listScopes.js";
import { useUpdateRoleMutation } from "@gram/client/react-query/updateRole.js";
import { Alert } from "@/components/ui/Alert";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { useOrgRoutes } from "@/routes";
import { ArrowLeft, Check, ChevronRight, Loader2 } from "lucide-react";
import { useMemo, useState } from "react";
import {
  getSelectableMembers,
  isMemberLockedToRole,
  membersWithRole,
} from "./changeRoleState";
import { GrantRuleDrawerContent } from "./GrantRuleDrawerContent";
import { PermissionScopeControl } from "./PermissionScopeControl";
import { RolePermissionsSection } from "./RolePermissionsSection";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import type { ActivePanel, ResourceType, RoleGrant, ScopeRule } from "./types";
import {
  isProjectSelectableResourceType,
  isUnrestrictedResourceType,
} from "./types";
import {
  isSaveDisabled,
  grantKeysString as grantKeysStringFn,
  computeRuleLabel,
} from "./roleDialogState";
import {
  applyRemoveRule,
  diffGrants,
  grantsFromRole,
  sdkGrantsFromForm,
} from "./roleGrantTransform";

// ─── Helpers ────────────────────────────────────────────────────────────────
//
// Grant ↔ form transforms live in ./roleGrantTransform — kept in a plain TS
// module so they can be unit-tested without pulling in React/react-query.

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

/** Map an allow level to the panels available for exception rules. */
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
      return []; // tool/annotation: already most specific, no exception possible
  }
}

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * How the editor is presented. A sheet is right for a quick change from the
 * roles list; a page is right for authoring, where a role's permissions and
 * their rules are taller than a sheet can hold.
 */
type RoleEditorPresentation = "sheet" | "page";

interface CreateRoleDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editingRole?: Role | null;
  onRoleCreated?: (roleName: string) => void;
  presentation?: RoleEditorPresentation;
}

export function CreateRoleDialog({
  open,
  onOpenChange,
  editingRole,
  onRoleCreated,
  presentation = "sheet",
}: CreateRoleDialogProps): JSX.Element {
  const isEditing = !!editingRole;
  const isSystemRole = !!editingRole?.isSystem;

  // ─── Form state ───────────────────────────────────────────────
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [grants, setGrants] = useState<Record<string, RoleGrant>>({});
  const [selectedMembers, setSelectedMembers] = useState<Set<string>>(
    new Set(),
  );
  const [initialMembers, setInitialMembers] = useState<Set<string>>(new Set());
  const [initialName, setInitialName] = useState("");
  const [initialDescription, setInitialDescription] = useState("");
  const [initialGrantKeys, setInitialGrantKeys] = useState("");
  const [showMembers, setShowMembers] = useState(false);
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
  const orgRoutes = useOrgRoutes();
  const { data: membersData } = useMembers();
  const members = [...(membersData?.members ?? [])].sort((a, b) =>
    a.name.localeCompare(b.name),
  );
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

  // ─── Initialize when editing ──────────────────────────────────
  if (editingRole && !initialized && scopesData && membersData) {
    setName(editingRole.name);
    setDescription(editingRole.description);
    const roleGrants = grantsFromRole(editingRole, scopesData.scopes);
    setGrants(roleGrants);
    setInitialName(editingRole.name);
    setInitialDescription(editingRole.description);
    setInitialGrantKeys(grantKeysStringFn(roleGrants));
    const assignedIds = new Set(membersWithRole(members, editingRole.id));
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

  const saveDisabled =
    !scopeDefinitions ||
    isSaveDisabled({
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
      setDraftRule({ ...grant.rules[ruleIndex]! });
    } else {
      // New exception rule, or edit the existing exception if one already exists.
      const existingDenyIdx = grant?.rules.findIndex(
        (r) => r.effect === "deny",
      );
      if (existingDenyIdx !== undefined && existingDenyIdx >= 0) {
        // Edit the existing exception rule instead of creating a new one.
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

  // Escape, the X, and the backdrop mean "leave it as it was", so they close
  // the editor without writing the draft back.
  const discardRuleEditor = () => {
    setDialogStep("form");
    setTimeout(() => {
      setEditingScopeSlug(null);
      setEditingRuleIndex(-1);
      setDraftRule(null);
    }, 300);
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
            const originalRule = grant.rules[editingRuleIndex];
            rules[editingRuleIndex] = draftRule;
            // Allow changed: clear exceptions scoped to the old allow.
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
            // One exception per scope: replace any existing exception.
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

  // "All servers" is the unrestricted rule, which the model stores as null
  // selectors rather than as a list naming everything.
  const resetRuleToAll = (scopeSlug: string) => {
    setGrants((prev) => {
      const grant = prev[scopeSlug];
      if (!grant) return prev;
      return {
        ...prev,
        [scopeSlug]: {
          ...grant,
          rules: grant.rules.map((rule) =>
            rule.effect === "allow" ? { ...rule, selectors: null } : rule,
          ),
        },
      };
    });
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

  // ─── Group operations ─────────────────────────────────────────

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
    const selectableMembers = getSelectableMembers(
      members,
      isEditing,
      editingRole?.id,
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
    if (!scopeDefinitions) return;

    const sdkGrants = sdkGrantsFromForm(grants, scopeDefinitions);

    if (isEditing) {
      const initialGrants = sdkGrantsFromForm(
        grantsFromRole(editingRole, scopeDefinitions),
        scopeDefinitions,
      );
      const { addGrants, removeGrants } = diffGrants(initialGrants, sdkGrants);

      updateRole.mutate({
        request: {
          updateRoleForm: {
            id: editingRole.id,
            // System role name/description are platform-managed; permissions
            // (grants) are per-org and editable.
            ...(isSystemRole
              ? { addGrants, removeGrants }
              : { name, description, addGrants, removeGrants }),
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
            description: description.trim() || undefined,
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
    setSelectedMembers(new Set());
    setInitialMembers(new Set());
    setInitialName("");
    setInitialDescription("");
    setInitialGrantKeys("");
    setShowMembers(false);
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

  // Exception constraints: an exception must be narrower than the broadest allow.
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

  // ─── Render ───────────────────────────────────────────────────

  const isPage = presentation === "page";
  const Frame = isPage ? PageFrame : SheetFrame;
  // Sheet chrome is Radix Dialog chrome: on a page there is no Dialog for it
  // to live in, so the same slots render as plain elements.
  const Header = isPage ? PageHeaderSlot : SheetHeader;
  const Title = isPage ? PageTitleSlot : SheetTitle;
  const Description = isPage ? PageDescriptionSlot : SheetDescription;
  const Footer = isPage ? PageFooterSlot : SheetFooter;

  return (
    <Frame open={open} onClose={handleClose}>
      {/* The sheet's header doubles as the rule editor's title bar. On a page
          the modal carries its own title, so rendering this header when the
          step changes only pushed the form down behind the modal. */}
      {!isPage && (
        <Header className={cn(!isPage && "border-border border-b")}>
          <Title>
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
              "Edit Role"
            ) : (
              "Create Role"
            )}
          </Title>
          {dialogStep === "rule-editor" && draftRule && (
            <Description className="text-muted-foreground mr-5 ml-7 line-clamp-2 text-xs">
              {draftRule.effect === "allow"
                ? "Choose which resources this role can access. Start broad — you can add exceptions later to restrict specific items."
                : "Exclude specific resources that the allow rule would otherwise permit."}
            </Description>
          )}
        </Header>
      )}

      <div className={cn("relative flex-1", !isPage && "overflow-hidden")}>
        <div
          className={cn(
            "flex h-full",
            !isPage && "transition-transform duration-300 ease-in-out",
            !isPage && stepOffset,
          )}
        >
          {/* ─── Panel 1: Role form ─── */}
          <div
            className={cn(
              "w-full shrink-0 space-y-4 overflow-y-auto",
              // On a page the surrounding layout already sets the gutter;
              // adding the sheet's would indent the form past the title.
              isPage ? "pt-2" : "px-4 pt-3",
            )}
          >
            {organization.scimEnabled && (
              <Alert variant="info" dismissible={false} className="text-sm">
                Assign this role from{" "}
                <Link
                  to={orgRoutes.identity.href()}
                  className="whitespace-nowrap underline underline-offset-2"
                >
                  Identity → SCIM → Configure
                </Link>
                .
              </Alert>
            )}
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
                  disabled={editingRole?.isSystem}
                  placeholder="Describe what this role can do..."
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  className="border-input placeholder:text-muted-foreground focus-visible:ring-ring flex w-full resize-none border bg-transparent px-3 py-2 text-sm focus-visible:ring-1 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
                />
              )}
            />

            {isSystemRole && (
              // Quieter than a banner: the fields it describes are right
              // above it, and already visibly disabled.
              <Text muted small>
                Built-in role. Gram manages its name and description; its
                permissions are yours to change.
              </Text>
            )}

            {/* ─── Permissions ─── */}
            <RolePermissionsSection
              groups={scopeGroups}
              selectedScopes={new Set(Object.keys(grants))}
              disabled={false}
              onToggleScope={toggleScope}
              renderScopeRule={(scopeDef) => {
                const grant = grants[scopeDef.slug];
                if (!grant) return null;
                // Scopes with nothing to narrow — an organization is not a
                // list you pick from — carry no control at all: an "All" chip
                // that cannot be changed is noise.
                if (isUnrestrictedResourceType(scopeDef.resourceType)) {
                  return null;
                }
                const allowIndex = grant.rules.findIndex(
                  (rule) => rule.effect === "allow",
                );
                const allowRule = grant.rules[allowIndex];
                const denyRules = grant.rules
                  .map((rule, index) => ({ rule, index }))
                  .filter(({ rule }) => rule.effect === "deny");
                return (
                  <PermissionScopeControl
                    allowRule={allowRule}
                    denyRules={denyRules}
                    resourceType={scopeDef.resourceType}
                    allowLabel={computeRuleLabel(
                      allowRule?.selectors ?? null,
                      scopeDef.resourceType,
                      projectList,
                    )}
                    denyLabel={(rule) =>
                      computeRuleLabel(
                        rule.selectors,
                        scopeDef.resourceType,
                        projectList,
                      )
                    }
                    canAddException={
                      denyRules.length === 0 &&
                      getDenyPanels(
                        getAllowLevel(grant.rules),
                        isProjectSelectableResourceType(scopeDef.resourceType),
                      ).length > 0
                    }
                    disabled={false}
                    onChooseSpecific={() =>
                      openRuleEditor(scopeDef.slug, allowIndex)
                    }
                    onResetToAll={() => resetRuleToAll(scopeDef.slug)}
                    onAddException={() => openRuleEditor(scopeDef.slug, -1)}
                    onEditException={(index) =>
                      openRuleEditor(scopeDef.slug, index)
                    }
                    onRemoveException={(index) =>
                      removeRule(scopeDef.slug, index)
                    }
                  />
                );
              }}
            />

            {/* ─── Assign Members (hidden when directory sync manages assignment) ─── */}
            {!organization.scimEnabled && (
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
                  <Text variant="body" className="font-medium">
                    Assign Members
                  </Text>
                  <Text variant="body" className="text-muted-foreground ml-1">
                    (optional, {selectedMembers.size} selected)
                  </Text>
                </button>

                {showMembers && (
                  <div className="border-border divide-border mt-3 divide-y border">
                    {/* Select-all header */}
                    {(() => {
                      const selectableMembers = getSelectableMembers(
                        members,
                        isEditing,
                        editingRole?.id,
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
                          <Text
                            variant="body"
                            className="text-muted-foreground text-sm font-medium"
                          >
                            Select all
                          </Text>
                        </label>
                      );
                    })()}
                    {members.map((member) => {
                      const alreadyHasRole = isMemberLockedToRole(
                        isEditing,
                        editingRole?.id,
                        member.roleIds,
                      );
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
                            onCheckedChange={() => {
                              void (!alreadyHasRole && toggleMember(member.id));
                            }}
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
                            <Text
                              variant="body"
                              className="text-sm font-medium"
                            >
                              {member.name}
                            </Text>
                            <Text
                              variant="body"
                              className="text-muted-foreground text-xs"
                            >
                              {member.email}
                            </Text>
                          </div>
                        </label>
                      );
                    })}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* ─── Panel 2: Rule editor ─── */}
          {/* In a sheet it slides in as a second step. On a page there is no
              second step to slide to, so the same content opens as a modal
              over the row it belongs to. */}
          <RuleEditorFrame
            isPage={isPage}
            open={dialogStep === "rule-editor"}
            title={`${editingRuleIndex >= 0 ? "Edit" : "Create"} ${
              draftRule?.effect === "allow" ? "allow" : "exception"
            } rule`}
            description={
              draftRule?.effect === "allow"
                ? "Choose which resources this role can access. Start broad — you can add exceptions later to restrict specific items."
                : "Exclude specific resources that the allow rule would otherwise permit."
            }
            onDone={saveAndCloseRuleEditor}
            onDismiss={discardRuleEditor}
          >
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
              </>
            )}
          </RuleEditorFrame>
        </div>
      </div>

      {dialogStep === "rule-editor" && !isPage && (
        <Footer className="border-border flex-row justify-end border-t">
          <Button variant="primary" onClick={saveAndCloseRuleEditor}>
            <Button.LeftIcon>
              <Check className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Done</Button.Text>
          </Button>
        </Footer>
      )}

      {(dialogStep === "form" || isPage) && (
        <Footer className="border-border flex-row justify-end border-t">
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
        </Footer>
      )}
    </Frame>
  );
}

/**
 * Where the rule editor lives. The sheet slides it in as a second step; the
 * page opens it as a modal, so the page keeps the role in view behind it.
 */
function RuleEditorFrame({
  isPage,
  open,
  title,
  description,
  onDone,
  onDismiss,
  children,
}: {
  isPage: boolean;
  open: boolean;
  title: string;
  description: string;
  onDone: () => void;
  /** Escape, the X, or the backdrop: leave the rule as it was. */
  onDismiss: () => void;
  children: React.ReactNode;
}): JSX.Element {
  if (!isPage) {
    return (
      <div className="flex w-full shrink-0 flex-col overflow-hidden">
        {children}
      </div>
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onDismiss();
      }}
    >
      <Dialog.Content className="flex max-h-[80vh] flex-col sm:max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
        </Dialog.Header>
        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto py-2">
          {children}
        </div>
        <div className="flex justify-end pt-2">
          <Button variant="primary" onClick={onDone}>
            <Button.LeftIcon>
              <Check className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Done</Button.Text>
          </Button>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

/** Sheet chrome: the editor opened over the roles list. */
function SheetFrame({
  open,
  onClose,
  children,
}: {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onClose}>
      <SheetContent
        side="right"
        className={cn(
          "flex w-full flex-col gap-1 overflow-hidden sm:max-w-2xl",
        )}
      >
        {children}
      </SheetContent>
    </Sheet>
  );
}

function PageHeaderSlot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className={cn("flex flex-col gap-1 pb-4", className)}>{children}</div>
  );
}

function PageTitleSlot({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  return <h2 className="text-display-xs font-thin">{children}</h2>;
}

function PageDescriptionSlot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <p className={cn("text-muted-foreground text-sm", className)}>{children}</p>
  );
}

function PageFooterSlot({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className={cn("flex flex-row justify-end gap-2 pt-6", className)}>
      {children}
    </div>
  );
}

/** Page chrome: the editor as its own route, with room to work. */
function PageFrame({
  children,
}: {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
}): JSX.Element {
  return <div className="flex w-full flex-col gap-1">{children}</div>;
}

// ─── Sub-components ─────────────────────────────────────────────────────────

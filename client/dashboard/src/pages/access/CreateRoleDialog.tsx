import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { Role } from "@gram/client/models/components/role.js";
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
import { ArrowRight, ChevronRight, Info, Loader2 } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useMemo, useState } from "react";
import { ScopePickerPopover } from "./ScopePickerPopover";
import type {
  AnnotationHint,
  CustomTab,
  RoleGrant,
  Scope,
  Selector,
} from "./types";
import { DISPOSITION_TO_ANNOTATION } from "./types";
import { isSaveDisabled } from "./roleDialogState";

interface CreateRoleDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editingRole?: Role | null;
  onRoleCreated?: (roleName: string) => void;
}

function grantsFromRole(role: Role): Record<string, RoleGrant> {
  const result: Record<string, RoleGrant> = {};
  for (const g of role.grants) {
    result[g.scope] = { scope: g.scope, selectors: g.selectors ?? null };
  }
  return result;
}

/**
 * Infer which custom tab was used from saved selectors by inspecting
 * their keys — disposition selectors → "auto-groups", tool selectors → "select".
 */
function inferCustomTab(selectors: Selector[]): {
  tab: CustomTab;
  annotations?: AnnotationHint[];
} {
  if (!selectors.length) return { tab: "select" };

  // Check for disposition selectors → "auto-groups" tab
  const dispositionSelectors = selectors.filter((s) => s.disposition);
  if (dispositionSelectors.length > 0) {
    const annotations = dispositionSelectors
      .map((s) =>
        s.disposition ? DISPOSITION_TO_ANNOTATION[s.disposition] : undefined,
      )
      .filter((a): a is AnnotationHint => !!a);
    return { tab: "auto-groups", annotations };
  }

  return { tab: "select" };
}

export function CreateRoleDialog({
  open,
  onOpenChange,
  editingRole,
  onRoleCreated,
}: CreateRoleDialogProps) {
  const isEditing = !!editingRole;
  const isSystemRole = !!editingRole?.isSystem;
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  // Grants keyed by scope slug — presence means the scope is enabled
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

  const queryClient = useQueryClient();
  const { data: membersData } = useMembers();
  const members = membersData?.members ?? [];
  const { data: rolesData } = useRoles();
  const roleNameById = new Map(
    (rolesData?.roles ?? []).map((r) => [r.id, r.name]),
  );
  const { data: scopesData } = useListScopes();

  const scopeGroups = useMemo(() => {
    const scopes = scopesData?.scopes ?? [];
    const groupOrder: { label: string; resourceType: string }[] = [
      { label: "Organization", resourceType: "org" },
      { label: "Build & Deploy", resourceType: "project" },
      { label: "MCP Servers", resourceType: "mcp" },
    ];
    return groupOrder.map((g) => ({
      ...g,
      scopes: scopes.filter((s) => s.resourceType === g.resourceType),
    }));
  }, [scopesData]);

  // Pre-populate fields when editing — wait for async data so autoExpanded works correctly.
  if (editingRole && !initialized && scopesData && membersData) {
    setName(editingRole.name);
    setDescription(editingRole.description);
    const roleGrants = grantsFromRole(editingRole);
    // Auto-expand groups that have at least one scope selected
    const grantedScopes = new Set(Object.keys(roleGrants));
    const autoExpanded = new Set(
      scopeGroups
        .filter((g) => g.scopes.some((s) => grantedScopes.has(s.slug)))
        .map((g) => g.label),
    );
    setExpandedGroups(autoExpanded);
    // Restore custom tab hints for MCP scopes with tool/disposition selectors
    for (const [scope, grant] of Object.entries(roleGrants)) {
      if (!scope.startsWith("mcp:")) continue;
      const hasCustomSelectors =
        grant.selectors?.some((s) => s.tool || s.disposition) ?? false;
      if (!hasCustomSelectors) continue;
      const detected = inferCustomTab(grant.selectors ?? []);
      roleGrants[scope] = {
        ...grant,
        customTab: detected.tab,
        ...(detected.annotations ? { annotations: detected.annotations } : {}),
      };
    }
    setGrants(roleGrants);
    setInitialName(editingRole.name);
    setInitialDescription(editingRole.description);
    setInitialGrantKeys(Object.keys(roleGrants).sort().join(","));
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

  const grantCount = Object.values(grants).filter(
    (g) => g.selectors === null || g.selectors.length > 0,
  ).length;

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

  const toggleScope = (scope: Scope) => {
    setGrants((prev) => {
      const next = { ...prev };
      if (next[scope]) {
        delete next[scope];
      } else {
        next[scope] = { scope, selectors: null };
      }
      return next;
    });
  };

  const setGrantSelectors = (scope: Scope, selectors: Selector[] | null) => {
    setGrants((prev) => ({
      ...prev,
      [scope]: { ...prev[scope], scope, selectors },
    }));
  };

  const setGrantAnnotations = (scope: Scope, annotations: AnnotationHint[]) => {
    setGrants((prev) => ({
      ...prev,
      [scope]: { ...prev[scope], scope, annotations },
    }));
  };

  const toggleGroup = (label: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(label)) {
        next.delete(label);
      } else {
        next.add(label);
      }
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
          next[scope.slug] = { scope: scope.slug, selectors: null };
        }
      }
      return next;
    });
  };

  const toggleMember = (memberId: string) => {
    setSelectedMembers((prev) => {
      const next = new Set(prev);
      if (next.has(memberId)) {
        next.delete(memberId);
      } else {
        next.add(memberId);
      }
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
        if (allSelected) {
          next.delete(m.id);
        } else {
          next.add(m.id);
        }
      }
      return next;
    });
  };

  const handleSubmit = () => {
    const sdkGrants = Object.values(grants)
      // Drop scopes with an empty selector list — the user switched to
      // "specific" but didn't select anything, so there's nothing to grant.
      .filter((g) => g.selectors === null || g.selectors.length > 0)
      .map((g) => ({
        scope: g.scope,
        // Local type uses null for unrestricted; SDK uses undefined
        selectors: g.selectors === null ? undefined : g.selectors,
      }));

    if (isEditing) {
      updateRole.mutate({
        request: {
          updateRoleForm: {
            id: editingRole.id,
            // System roles are immutable in WorkOS — only member assignment is allowed.
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
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={handleClose}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-lg"
      >
        <SheetHeader>
          <SheetTitle>{isEditing ? "Edit Role" : "Create Role"}</SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-4 overflow-y-auto px-4">
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

          {/* Permissions / Grants */}
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
                    System role permissions are managed by Gram and cannot be
                    changed.
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
                          <Type variant="body" className="text-sm font-medium">
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

                            const row = (
                              <div
                                key={scopeDef.slug}
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

                                <div className="flex w-[110px] shrink-0 justify-end">
                                  {isChecked && !isSystemRole && (
                                    <ScopePickerPopover
                                      resourceType={scopeDef.resourceType}
                                      scope={scopeDef.slug}
                                      selectors={grant.selectors}
                                      onChangeSelectors={(selectors) =>
                                        setGrantSelectors(
                                          scopeDef.slug,
                                          selectors,
                                        )
                                      }
                                      annotations={grant.annotations}
                                      onChangeAnnotations={(annotations) =>
                                        setGrantAnnotations(
                                          scopeDef.slug,
                                          annotations,
                                        )
                                      }
                                      customTab={grant.customTab}
                                      onCustomTabChange={(tab) =>
                                        setGrants((prev) => ({
                                          ...prev,
                                          [scopeDef.slug]: {
                                            ...prev[scopeDef.slug]!,
                                            customTab: tab,
                                          },
                                        }))
                                      }
                                    />
                                  )}
                                </div>
                              </div>
                            );

                            if (isSystemRole) {
                              return (
                                <Tooltip key={scopeDef.slug}>
                                  <TooltipTrigger asChild>{row}</TooltipTrigger>
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

          {/* Assign Members */}
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
                    selectableMembers.every((m) => selectedMembers.has(m.id));
                  const someSelected =
                    !allSelected &&
                    selectableMembers.some((m) => selectedMembers.has(m.id));
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
                          <Type variant="body" className="text-sm font-medium">
                            {member.name}
                          </Type>
                          {member.roleId && roleNameById.get(member.roleId) && (
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
                  ? "Saving…"
                  : "Creating…"
                : isEditing
                  ? "Save Changes"
                  : "Create Role"}
            </Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

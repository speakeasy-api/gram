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
import { ArrowRight, ChevronRight, Loader2 } from "lucide-react";
import { useMemo, useState } from "react";
import { ScopePickerPopover } from "./ScopePickerPopover";
import type { AnnotationHint, CustomTab, RoleGrant, Scope } from "./types";

interface CreateRoleDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editingRole?: Role | null;
}

function grantsFromRole(role: Role): Record<string, RoleGrant> {
  const result: Record<string, RoleGrant> = {};
  for (const g of role.grants) {
    result[g.scope] = { scope: g.scope, resources: g.resources ?? null };
  }
  return result;
}

export function CreateRoleDialog({
  open,
  onOpenChange,
  editingRole,
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
  const [showMembers, setShowMembers] = useState(false);
  const [showPermissions, setShowPermissions] = useState(true);
  const [initialized, setInitialized] = useState(false);
  // Track which MCP scopes have "Custom" mode selected (UI-only state)
  const [customScopes, setCustomScopes] = useState<Set<string>>(new Set());

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

  // Pre-populate fields when editing
  if (editingRole && !initialized) {
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
    // Restore custom mode and active tab for MCP scopes with tool-level selections
    const restoredCustom = new Set<string>();
    for (const [scope, grant] of Object.entries(roleGrants)) {
      if (!scope.startsWith("mcp:")) continue;
      const hasAnnotations = grant.annotations && grant.annotations.length > 0;
      const hasToolIds = grant.resources?.some((r) => r.includes(":")) ?? false;
      if (hasAnnotations || hasToolIds) {
        restoredCustom.add(scope);
        // Infer which tab was active
        let tab: CustomTab = "select";
        if (hasAnnotations) tab = "auto-groups";
        else if (hasToolIds) tab = "http-method";
        roleGrants[scope] = { ...grant, customTab: tab };
      }
    }
    setGrants(roleGrants);
    setCustomScopes(restoredCustom);
    const assignedIds = new Set(
      members.filter((m) => m.roleId === editingRole.id).map((m) => m.id),
    );
    setSelectedMembers(assignedIds);
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
    (g) => g.resources === null || g.resources.length > 0,
  ).length;

  const toggleScope = (scope: Scope) => {
    setGrants((prev) => {
      const next = { ...prev };
      if (next[scope]) {
        delete next[scope];
      } else {
        next[scope] = { scope, resources: null };
      }
      return next;
    });
  };

  const setGrantResources = (scope: Scope, resources: string[] | null) => {
    setGrants((prev) => ({
      ...prev,
      [scope]: { ...prev[scope], scope, resources },
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

    const allSelected = group.scopes.every((s) => grants[s.slug]);

    setGrants((prev) => {
      const next = { ...prev };
      for (const scope of group.scopes) {
        if (allSelected) {
          delete next[scope.slug];
        } else if (!next[scope.slug]) {
          next[scope.slug] = { scope: scope.slug, resources: null };
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

  const handleSubmit = () => {
    const sdkGrants = Object.values(grants)
      // Drop scopes with an empty resource list — the user switched to
      // "specific" but didn't select any resources, so there's nothing to grant.
      .filter((g) => g.resources === null || g.resources.length > 0)
      .map((g) => ({
        scope: g.scope,
        // Local type uses null for unrestricted; SDK uses undefined
        resources: g.resources === null ? undefined : g.resources,
      }));

    if (isEditing) {
      updateRole.mutate({
        request: {
          updateRoleForm: {
            id: editingRole.id,
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
    setShowMembers(false);
    setShowPermissions(true);
    setCustomScopes(new Set());
    setInitialized(false);
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={handleClose}>
      <SheetContent
        side="right"
        className="sm:max-w-lg w-full flex flex-col overflow-hidden"
      >
        <SheetHeader>
          <SheetTitle>{isEditing ? "Edit Role" : "Create Role"}</SheetTitle>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-4 space-y-4">
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
                className="flex w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 resize-none"
              />
            )}
          />

          {/* Permissions / Grants */}
          <div className="border-t border-border pt-4">
            <button
              type="button"
              onClick={() => setShowPermissions(!showPermissions)}
              className="flex items-center gap-1 w-full text-left"
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
                      className="border border-border rounded-md"
                    >
                      {/* Group header */}
                      <div className="flex items-center justify-between px-3 py-2">
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
                            onCheckedChange={() =>
                              toggleGroupCheckbox(group.label)
                            }
                          />
                          <button
                            type="button"
                            onClick={() => toggleGroup(group.label)}
                            className="flex items-center gap-1"
                          >
                            <Type
                              variant="body"
                              className="font-medium text-sm"
                            >
                              {group.label}
                            </Type>
                            <Type
                              variant="body"
                              className="text-muted-foreground text-sm"
                            >
                              ({selectedInGroup}/{group.scopes.length})
                            </Type>
                          </button>
                        </div>
                        <button
                          type="button"
                          onClick={() => toggleGroup(group.label)}
                        >
                          <ChevronRight
                            className={cn(
                              "h-3.5 w-3.5 transition-transform text-muted-foreground",
                              isExpanded && "rotate-90",
                            )}
                          />
                        </button>
                      </div>

                      {/* Expanded scope rows */}
                      {isExpanded && (
                        <div className="border-t border-border bg-muted/40">
                          {group.scopes.map((scopeDef) => {
                            const grant = grants[scopeDef.slug];
                            const isChecked = !!grant;

                            return (
                              <div
                                key={scopeDef.slug}
                                className="flex items-start gap-3 px-3 py-2.5 hover:bg-muted/50"
                              >
                                <label className="flex items-start gap-3 flex-1 min-w-0 cursor-pointer">
                                  <Checkbox
                                    disabled={isSystemRole}
                                    checked={isChecked}
                                    onCheckedChange={() =>
                                      toggleScope(scopeDef.slug)
                                    }
                                    className="mt-0.5 bg-background"
                                  />
                                  <div className="flex-1 min-w-0">
                                    <Type
                                      variant="body"
                                      className="font-medium text-sm font-mono"
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

                                <div className="w-[110px] shrink-0 flex justify-end">
                                  {isChecked && !isSystemRole && (
                                    <ScopePickerPopover
                                      resourceType={scopeDef.resourceType}
                                      resources={grant.resources}
                                      onChangeResources={(resources) =>
                                        setGrantResources(
                                          scopeDef.slug,
                                          resources,
                                        )
                                      }
                                      customMode={customScopes.has(
                                        scopeDef.slug,
                                      )}
                                      onCustomModeChange={(custom) =>
                                        setCustomScopes((prev) => {
                                          const next = new Set(prev);
                                          if (custom) {
                                            next.add(scopeDef.slug);
                                          } else {
                                            next.delete(scopeDef.slug);
                                          }
                                          return next;
                                        })
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
          <div className="border-t border-border pt-4 pb-4">
            <button
              type="button"
              onClick={() => setShowMembers(!showMembers)}
              className="flex items-center gap-1 w-full text-left"
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
              <div className="mt-3 border border-border rounded-md divide-y divide-border">
                {members.map((member) => {
                  const alreadyHasRole =
                    isEditing && member.roleId === editingRole?.id;
                  return (
                    <label
                      key={member.id}
                      className={cn(
                        "flex items-center gap-3 px-3 py-2.5 hover:bg-muted/50 cursor-pointer",
                        alreadyHasRole && "opacity-50 cursor-default",
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
                      <div className="flex-1 min-w-0 space-y-0.5">
                        <div className="flex items-center gap-2">
                          <Type variant="body" className="font-medium text-sm">
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
                                    <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
                                    <Badge
                                      variant="outline"
                                      size="sm"
                                      className="font-mono text-[10px] uppercase border-primary text-primary"
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

        <SheetFooter className="border-t border-border flex-row justify-end">
          <Button variant="secondary" onClick={handleClose}>
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={
              !name.trim() ||
              !description.trim() ||
              grantCount === 0 ||
              isMutating
            }
          >
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

import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import type { Role } from "@gram/client/models/components/role.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import {
  invalidateAllMembers,
  useMembers,
} from "@gram/client/react-query/members.js";
import { useDeleteRoleMutation } from "@gram/client/react-query/deleteRole.js";
import { SkeletonTable } from "@/components/ui/skeleton";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { CreateRoleDialog } from "./CreateRoleDialog";
import { DeleteRoleDialog } from "./DeleteRoleDialog";
import { MemberFacepile } from "@/components/member-facepile";
import { Ellipsis, Plus } from "lucide-react";
import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { visiblePermissionCount } from "./roleDialogState";

function RoleActionsMenu({
  role,
  onEdit,
  onDelete,
}: {
  role: Role;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    // Render-prop form: the dropdown content is portaled to <body>, so it
    // escapes the pointer-events containment of the node form. Disabling the
    // Radix trigger directly is what actually prevents the menu from opening.
    <RequireScope scope="org:admin" level="component">
      {({ disabled }) => (
        <DropdownMenu open={open} onOpenChange={setOpen} modal={false}>
          <DropdownMenuTrigger asChild disabled={disabled}>
            <button
              type="button"
              disabled={disabled}
              className={cn(
                "text-muted-foreground hover:bg-accent hover:text-foreground flex h-8 w-8 cursor-pointer items-center justify-center transition-colors",
                open && "bg-accent text-foreground",
                disabled && "cursor-not-allowed",
              )}
            >
              <Ellipsis className="h-4 w-4" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onSelect={() => {
                void setTimeout(onEdit, 0);
              }}
            >
              Edit
            </DropdownMenuItem>
            {!role.isSystem && (
              <DropdownMenuItem
                onSelect={() => {
                  void setTimeout(onDelete, 0);
                }}
              >
                Delete
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </RequireScope>
  );
}

type RowMember = {
  id: string;
  name: string;
  email: string;
  photoUrl?: string;
  roleIds: string[];
};

function RoleRow({
  role,
  members,
  canManageRoles,
  onEdit,
  onDelete,
}: {
  role: Role;
  members: RowMember[];
  canManageRoles: boolean;
  onEdit: () => void;
  onDelete: () => void;
}): JSX.Element {
  const roleMembers = members
    .filter((m) => m.roleIds.includes(role.id))
    .map((m) => ({
      id: m.id,
      name: m.name,
      email: m.email,
      photoUrl: m.photoUrl,
    }));

  return (
    <div
      role={canManageRoles ? "button" : undefined}
      tabIndex={canManageRoles ? 0 : undefined}
      onClick={canManageRoles ? onEdit : undefined}
      onKeyDown={
        canManageRoles
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onEdit();
              }
            }
          : undefined
      }
      className={cn(
        "border-border col-span-full grid grid-cols-subgrid items-center gap-x-6 border-b px-4 py-3 last:border-b-0",
        canManageRoles && "hover:bg-muted/50 cursor-pointer",
      )}
    >
      <div className="flex items-center gap-2">
        <Type variant="body" className="font-medium">
          {role.name}
        </Type>
        {role.isSystem && (
          <Badge variant="neutral" background={false} size="sm">
            System
          </Badge>
        )}
      </div>
      <Type variant="body" className="text-muted-foreground min-w-0 truncate">
        {role.description}
      </Type>
      <Type variant="body">{visiblePermissionCount(role.grants)}</Type>
      <MemberFacepile members={roleMembers} />
      <div aria-hidden />
      <div onClick={(e) => e.stopPropagation()} className="flex justify-end">
        <RoleActionsMenu role={role} onEdit={onEdit} onDelete={onDelete} />
      </div>
    </div>
  );
}

export function RolesTab(): JSX.Element {
  const { hasAnyScope } = useRBAC();
  // Mirror the row-actions menu gate (org:admin): without it, the row is not
  // clickable and shows no affordance.
  const canManageRoles = hasAnyScope(["org:admin"]);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<Role | null>(null);
  const [deletingRole, setDeletingRole] = useState<Role | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const { data: rolesData, isLoading } = useRoles();
  const roles = [...(rolesData?.roles ?? [])].sort(
    (a, b) => Number(b.isSystem) - Number(a.isSystem),
  );
  const { data: membersData } = useMembers();
  const members = membersData?.members ?? [];

  useEffect(() => {
    const editRoleId = searchParams.get("editRole");
    if (editRoleId && roles.length > 0) {
      const role = roles.find((r) => r.id === editRoleId);
      if (role) {
        setEditingRole(role);
        setSearchParams(
          (prev) => {
            prev.delete("editRole");
            return prev;
          },
          { replace: true },
        );
      }
    }
  }, [searchParams, roles, setSearchParams]);

  const defaultRole =
    roles.find((r) => r.isSystem && r.name === "Member") ?? null;

  const membersOfDeletingRole = deletingRole
    ? members.filter((m) => m.roleIds.includes(deletingRole.id))
    : [];

  const deleteRole = useDeleteRoleMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRoles(queryClient),
        invalidateAllMembers(queryClient),
      ]);
    },
  });

  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <div>
          <Heading variant="h4">Roles</Heading>
          <Type muted small className="mt-1">
            Define roles and their associated permissions.
          </Type>
        </div>
        <RequireScope scope="org:admin" level="component">
          <Button onClick={() => setIsCreateOpen(true)}>
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Add Role</Button.Text>
          </Button>
        </RequireScope>
      </div>

      {isLoading ? (
        <div className="mt-4">
          <SkeletonTable />
        </div>
      ) : (
        // Grid table: the parent owns the column tracks and each row is a
        // subgrid spanning them, so cells align across rows. Description uses
        // minmax(0,1fr) (shrinks, absorbs slack); Members uses max-content
        // (sizes to the bounded facepile) — neither can overflow the table.
        <div className="border-border mt-4 grid grid-cols-[max-content_minmax(0,24rem)_max-content_max-content_1fr_max-content] overflow-hidden border">
          <div className="text-muted-foreground border-border col-span-full grid grid-cols-subgrid items-center gap-x-6 border-b px-4 py-2.5 text-sm">
            <div>Name</div>
            <div>Description</div>
            <div>Permissions</div>
            <div>Members</div>
            <div aria-hidden />
            <div className="sr-only">Actions</div>
          </div>
          {roles.length === 0 ? (
            <div className="text-muted-foreground col-span-full p-4">
              No roles have been created yet.
            </div>
          ) : (
            roles.map((role) => (
              <RoleRow
                key={role.id}
                role={role}
                members={members}
                canManageRoles={canManageRoles}
                onEdit={() => setEditingRole(role)}
                onDelete={() => setDeletingRole(role)}
              />
            ))
          )}
        </div>
      )}

      <Card className="bg-muted/30 mt-8 gap-0 p-4">
        <Type variant="subheading" className="mb-4">
          About System roles
        </Type>
        <div className="flex items-start gap-3 text-sm">
          <Badge
            variant="neutral"
            background={false}
            size="sm"
            className="bg-background mt-0.5 w-16 shrink-0 justify-center"
          >
            Member
          </Badge>
          <Type variant="body" className="text-muted-foreground text-sm">
            The default role for most users. Grants read access across the
            organization and projects. Gives the ability to connect to MCP
            servers and other resources.
          </Type>
        </div>
        <div className="mt-2 flex items-start gap-3 text-sm">
          <Badge
            variant="neutral"
            background={false}
            size="sm"
            className="bg-background mt-0.5 w-16 shrink-0 justify-center"
          >
            Admin
          </Badge>
          <Type variant="body" className="text-muted-foreground text-sm">
            Full access to all organization settings, billing, member
            management, every project, MCP server, skills and assistants.
          </Type>
        </div>
      </Card>

      <CreateRoleDialog
        open={isCreateOpen || !!editingRole}
        onOpenChange={(open) => {
          if (!open) {
            setIsCreateOpen(false);
            setEditingRole(null);
          }
        }}
        editingRole={editingRole}
      />

      <DeleteRoleDialog
        isOpen={!!deletingRole}
        onOpenChange={(open) => {
          if (!open) setDeletingRole(null);
        }}
        handleDeleteRole={() => {
          void (async () => {
            if (deletingRole) {
              await deleteRole.mutateAsync({
                request: { id: deletingRole.id },
              });
              setDeletingRole(null);
            }
          })();
        }}
        handleCancel={() => setDeletingRole(null)}
        role={deletingRole}
        members={membersOfDeletingRole}
        defaultRole={defaultRole}
      />
    </div>
  );
}

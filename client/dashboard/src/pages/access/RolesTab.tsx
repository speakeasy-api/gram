import { Badge } from "@/components/ui/badge";
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
  Button,
  Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Table,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { CreateRoleDialog } from "./CreateRoleDialog";
import { DeleteRoleDialog } from "./DeleteRoleDialog";
import { Ellipsis } from "lucide-react";
import { RequireScope } from "@/components/require-scope";

export function RolesTab() {
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<Role | null>(null);
  const [deletingRole, setDeletingRole] = useState<Role | null>(null);
  const queryClient = useQueryClient();
  const { data: rolesData, isLoading } = useRoles();
  const roles = rolesData?.roles ?? [];
  const { data: membersData } = useMembers();
  const members = membersData?.members ?? [];

  const defaultRole =
    roles.find((r) => r.isSystem && r.name === "Member") ?? null;
  const membersOfDeletingRole = deletingRole
    ? members.filter((m) => m.roleId === deletingRole.id)
    : [];

  const deleteRole = useDeleteRoleMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRoles(queryClient),
        invalidateAllMembers(queryClient),
      ]);
    },
  });

  const columns: Column<Role>[] = [
    {
      key: "name",
      header: "Name",
      width: "180px",
      render: (role) => (
        <div className="flex items-center gap-2">
          <Type variant="body" className="font-medium">
            {role.name}
          </Type>
          {role.isSystem && (
            <Badge variant="outline" size="sm">
              System
            </Badge>
          )}
        </div>
      ),
    },
    {
      key: "description",
      header: "Description",
      width: "1fr",
      render: (role) => (
        <Type variant="body" className="text-muted-foreground">
          {role.description}
        </Type>
      ),
    },
    {
      key: "permissions",
      header: "Permissions",
      width: "120px",
      render: (role) => <Type variant="body">{role.grants.length}</Type>,
    },
    {
      key: "members",
      header: "Members",
      width: "100px",
      render: (role) => <Type variant="body">{role.memberCount}</Type>,
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (role) => (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="tertiary"
              size="sm"
              className="opacity-50 hover:opacity-100"
            >
              <Button.LeftIcon>
                <Ellipsis className="h-4 w-4" />
              </Button.LeftIcon>
              <span className="hidden">Actions</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <RequireScope scope="org:admin" level="component">
              <DropdownMenuItem
                className="cursor-pointer"
                onSelect={() => setTimeout(() => setEditingRole(role), 0)}
              >
                Edit
              </DropdownMenuItem>
            </RequireScope>
            {!role.isSystem && (
              <RequireScope scope="org:admin" level="component">
                <DropdownMenuItem
                  className="text-destructive focus:text-destructive cursor-pointer"
                  onSelect={() => setTimeout(() => setDeletingRole(role), 0)}
                >
                  Delete
                </DropdownMenuItem>
              </RequireScope>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ];

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
              <Icon name="plus" className="h-4 w-4" />
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
        <Table
          columns={columns}
          data={roles}
          rowKey={(row) => row.id}
          className="mt-4"
        />
      )}

      <div className="border-border/50 bg-muted/30 mt-12 rounded-md border px-4 py-3">
        <Type variant="subheading" className="mb-4">
          About System roles
        </Type>
        <div className="flex items-start gap-3 text-sm">
          <Badge
            variant="outline"
            size="sm"
            className="mt-0.5 w-16 shrink-0 justify-center bg-white dark:bg-zinc-900"
          >
            Member
          </Badge>
          <Type variant="body" className="text-muted-foreground text-sm">
            The default role for most users. Grants read access across the
            organization and the ability to connect to MCP servers.
          </Type>
        </div>
        <div className="mt-2 flex items-start gap-3 text-sm">
          <Badge
            variant="outline"
            size="sm"
            className="mt-0.5 w-16 shrink-0 justify-center bg-white dark:bg-zinc-900"
          >
            Admin
          </Badge>
          <Type variant="body" className="text-muted-foreground text-sm">
            Full access to all organization settings, billing, member
            management, and every project and MCP server.
          </Type>
        </div>
      </div>

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
        handleDeleteRole={async () => {
          if (deletingRole) {
            await deleteRole.mutateAsync({ request: { id: deletingRole.id } });
            setDeletingRole(null);
          }
        }}
        handleCancel={() => setDeletingRole(null)}
        role={deletingRole}
        members={membersOfDeletingRole}
        defaultRole={defaultRole}
      />
    </div>
  );
}

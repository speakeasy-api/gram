import { Badge } from "@/components/ui/badge";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import type { Role } from "@gram/client/models/components/role.js";
import {
  invalidateAllListRoles,
  useListRoles,
} from "@gram/client/react-query/listRoles.js";
import { useDeleteRoleMutation } from "@gram/client/react-query/deleteRole.js";
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

export function RolesTab() {
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<Role | null>(null);
  const queryClient = useQueryClient();
  const { data: rolesData, isLoading } = useListRoles();
  const roles = rolesData?.roles ?? [];

  const deleteRole = useDeleteRoleMutation({
    onSuccess: async () => {
      await invalidateAllListRoles(queryClient);
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
              <Icon name="ellipsis" className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              className="cursor-pointer"
              onSelect={() => setTimeout(() => setEditingRole(role), 0)}
            >
              Edit
            </DropdownMenuItem>
            {!role.isSystem && (
              <DropdownMenuItem
                className="text-destructive focus:text-destructive cursor-pointer"
                onSelect={() => deleteRole.mutate({ request: { id: role.id } })}
              >
                Delete
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ];

  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <div>
          <Heading variant="h4">Roles</Heading>
          <Type muted small className="mt-1">
            Define roles and their associated permissions.
          </Type>
        </div>
        <Button onClick={() => setIsCreateOpen(true)}>
          <Button.LeftIcon>
            <Icon name="plus" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Add Role</Button.Text>
        </Button>
      </div>

      {isLoading ? (
        <div className="flex justify-center py-12">
          <Type muted>Loading roles...</Type>
        </div>
      ) : (
        <Table
          columns={columns}
          data={roles}
          rowKey={(row) => row.id}
          className="mt-4"
        />
      )}

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
    </div>
  );
}

import { Badge } from "@/components/ui/badge";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { Button, Column, Icon, Table } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { CreateRoleDialog } from "./CreateRoleDialog";
import { MOCK_ROLES } from "./mock-data";
import type { Role } from "./types";

export function RolesTab() {
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const roles = MOCK_ROLES;

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
      width: "50px",
      render: (role) => (
        <Button
          variant="tertiary"
          size="sm"
          disabled={role.isSystem}
          className="opacity-50 hover:opacity-100"
        >
          <Icon name="ellipsis" className="h-4 w-4" />
        </Button>
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

      <Table
        columns={columns}
        data={roles}
        rowKey={(row) => row.id}
        className="mt-4"
      />

      <CreateRoleDialog open={isCreateOpen} onOpenChange={setIsCreateOpen} />
    </div>
  );
}

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
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import { useChallengeRowColumns } from "./useChallengeRowColumns";
import { useGrantFlow } from "./useGrantFlow";

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
    <RequireScope scope="org:admin" level="component">
      <DropdownMenu open={open} onOpenChange={setOpen} modal={false}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className={cn(
              "text-muted-foreground hover:bg-accent hover:text-foreground flex h-8 w-8 cursor-pointer items-center justify-center rounded-md transition-colors",
              open && "bg-accent text-foreground",
            )}
          >
            <Ellipsis className="h-4 w-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => setTimeout(onEdit, 0)}>
            Edit
          </DropdownMenuItem>
          {!role.isSystem && (
            <DropdownMenuItem onSelect={() => setTimeout(onDelete, 0)}>
              Delete
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </RequireScope>
  );
}

export function RolesTab() {
  const orgRoutes = useOrgRoutes();
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<Role | null>(null);
  const [deletingRole, setDeletingRole] = useState<Role | null>(null);
  const { actionsColumn, grantFlowPortals } = useGrantFlow();
  const challengeRowColumns = useChallengeRowColumns();
  const queryClient = useQueryClient();
  const { data: rolesData, isLoading } = useRoles();
  const roles = [...(rolesData?.roles ?? [])].sort(
    (a, b) => Number(b.isSystem) - Number(a.isSystem),
  );
  const { data: membersData } = useMembers();
  const members = membersData?.members ?? [];

  const defaultRole =
    roles.find((r) => r.isSystem && r.name === "Member") ?? null;

  const { data: challengesData } = useChallenges({ limit: 5 });
  const recentChallenges = (challengesData?.challenges ?? []).filter(
    (c) => !!c.scope,
  );

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
        <RoleActionsMenu
          role={role}
          onEdit={() => setEditingRole(role)}
          onDelete={() => setDeletingRole(role)}
        />
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

      <div className="border-border/50 bg-muted/30 mt-8 rounded-md border px-4 py-3">
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

      {/* Recent Challenges */}
      <div className="mt-12">
        <div className="mb-3 flex items-center justify-between">
          <Heading variant="h4">Recent Challenges</Heading>
          <orgRoutes.access.challenges.Link className="text-primary cursor-pointer text-sm font-medium hover:underline">
            Show more
          </orgRoutes.access.challenges.Link>
        </div>
        <Table
          columns={[...challengeRowColumns, actionsColumn]}
          data={recentChallenges}
          rowKey={(row) => row.id}
        />
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

      {grantFlowPortals}

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

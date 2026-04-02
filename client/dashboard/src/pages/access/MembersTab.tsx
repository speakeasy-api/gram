import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Button, Column, Icon, Table } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { ChangeRoleDialog } from "./ChangeRoleDialog";

function getInitials(name: string) {
  return name
    .split(" ")
    .map((n) => n[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

export function MembersTab() {
  const [changingMember, setChangingMember] = useState<AccessMember | null>(
    null,
  );
  const { data: membersData, isLoading: membersLoading } = useMembers();
  const { data: rolesData } = useRoles();
  const members = membersData?.members ?? [];
  const roles = rolesData?.roles ?? [];

  const getRoleName = (roleId: string) =>
    roles.find((r) => r.id === roleId)?.name ?? "Unknown";

  const columns: Column<AccessMember>[] = [
    {
      key: "member",
      header: "Member",
      width: "200px",
      render: (member) => (
        <div className="flex items-center gap-3">
          <Avatar className="h-8 w-8">
            {member.photoUrl && (
              <AvatarImage src={member.photoUrl} alt={member.name} />
            )}
            <AvatarFallback className="text-xs">
              {getInitials(member.name)}
            </AvatarFallback>
          </Avatar>
          <Type variant="body" className="font-medium">
            {member.name}
          </Type>
        </div>
      ),
    },
    {
      key: "email",
      header: "Email",
      width: "1fr",
      render: (member) => (
        <Type variant="body" className="text-muted-foreground">
          {member.email}
        </Type>
      ),
    },
    {
      key: "role",
      header: "Role",
      width: "140px",
      render: (member) => (
        <Type variant="body">{getRoleName(member.roleId)}</Type>
      ),
    },
    {
      key: "joinedAt",
      header: "Joined",
      width: "160px",
      render: (member) => <HumanizeDateTime date={member.joinedAt} />,
    },
    {
      key: "actions",
      header: "",
      width: "100px",
      render: (member) => (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setChangingMember(member)}
        >
          <Button.Text className="text-primary">Change</Button.Text>
        </Button>
      ),
    },
  ];

  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <div>
          <Heading variant="h4">Team Members</Heading>
          <Type muted small className="mt-1">
            Manage role assignments for your team members.
          </Type>
        </div>
      </div>

      {membersLoading ? (
        <div className="mt-4">
          <SkeletonTable />
        </div>
      ) : (
        <Table
          columns={columns}
          data={members}
          rowKey={(row) => row.id}
          className="mt-4 rounded-b-none"
        />
      )}
      <div className="flex justify-center border border-t-0 border-border rounded-b-lg py-3">
        <Button variant="tertiary" size="sm">
          <Button.Text>Manage Team</Button.Text>
          <Button.RightIcon>
            <Icon name="arrow-right" className="h-4 w-4" />
          </Button.RightIcon>
        </Button>
      </div>

      <ChangeRoleDialog
        member={changingMember}
        onOpenChange={(open: boolean) => {
          if (!open) setChangingMember(null);
        }}
      />
    </div>
  );
}

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { Button, Column, Table } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { ChangeRoleDialog } from "./ChangeRoleDialog";
import { MOCK_MEMBERS, MOCK_ROLES } from "./mock-data";
import type { Member } from "./types";

function getInitials(name: string) {
  return name
    .split(" ")
    .map((n) => n[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

export function MembersTab() {
  const [changingMember, setChangingMember] = useState<Member | null>(null);
  const members = MOCK_MEMBERS;

  const getRoleName = (roleId: string) =>
    MOCK_ROLES.find((r) => r.id === roleId)?.name ?? "Unknown";

  const columns: Column<Member>[] = [
    {
      key: "member",
      header: "Member",
      width: "200px",
      render: (member) => (
        <div className="flex items-center gap-3">
          <Avatar className="h-8 w-8">
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
      render: (member) => <HumanizeDateTime date={new Date(member.joinedAt)} />,
    },
    {
      key: "actions",
      header: "",
      width: "80px",
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

      <Table
        columns={columns}
        data={members}
        rowKey={(row) => row.id}
        className="mt-4"
      />

      <ChangeRoleDialog
        member={changingMember}
        onOpenChange={(open) => {
          if (!open) setChangingMember(null);
        }}
      />
    </div>
  );
}

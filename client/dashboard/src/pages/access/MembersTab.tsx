import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
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
import { Ellipsis } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { ChangeRoleDialog } from "./ChangeRoleDialog";
import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useOrgRoutes } from "@/routes";

function getInitials(name: string) {
  return name
    .split(" ")
    .map((n) => n[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

function MemberActionsMenu({
  onChangeRole,
  onViewChallenges,
}: {
  onChangeRole: () => void;
  onViewChallenges: () => void;
}) {
  const [open, setOpen] = useState(false);

  return (
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
        <RequireScope scope="org:admin" level="component">
          <DropdownMenuItem onSelect={() => setTimeout(onChangeRole, 0)}>
            Change role
          </DropdownMenuItem>
        </RequireScope>
        <DropdownMenuItem onSelect={() => setTimeout(onViewChallenges, 0)}>
          View challenges
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function MembersTab() {
  const [changingMember, setChangingMember] = useState<AccessMember | null>(
    null,
  );
  const navigate = useNavigate();
  const organization = useOrganization();
  const telemetry = useTelemetry();
  const orgRoutes = useOrgRoutes();
  const isTeamPageEnabled =
    telemetry.isFeatureEnabled("gram-team-page") ?? false;
  const { data: membersData, isLoading: membersLoading } = useMembers();
  const { data: rolesData } = useRoles();
  const roles = rolesData?.roles ?? [];
  const members = [...(membersData?.members ?? [])].sort((a, b) => {
    const aSystem = roles.find((r) => r.id === a.roleId)?.isSystem ?? false;
    const bSystem = roles.find((r) => r.id === b.roleId)?.isSystem ?? false;
    return Number(bSystem) - Number(aSystem);
  });

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
      width: "60px",
      render: (member) => (
        <MemberActionsMenu
          onChangeRole={() => setChangingMember(member)}
          onViewChallenges={() => {
            navigate(
              `${orgRoutes.access.challenges.href()}?identity=${encodeURIComponent(member.email)}`,
            );
          }}
        />
      ),
    },
  ];

  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
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
      <div className="border-border flex justify-center rounded-b-lg border border-t-0 py-3">
        <RequireScope scope="org:admin" level="component">
          {isTeamPageEnabled ? (
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => orgRoutes.team.goTo()}
            >
              <Button.Text>Manage Team</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" className="h-4 w-4" />
              </Button.RightIcon>
            </Button>
          ) : (
            <Button variant="tertiary" size="sm" asChild>
              <a
                href={
                  organization?.userWorkspaceSlugs?.length
                    ? `https://app.speakeasy.com/org/${organization.slug}/${organization.userWorkspaceSlugs[0]}/settings/team`
                    : "https://app.speakeasy.com"
                }
                target="_blank"
                rel="noopener noreferrer"
              >
                <Button.Text>Manage Team</Button.Text>
                <Button.RightIcon>
                  <Icon name="external-link" className="h-4 w-4" />
                </Button.RightIcon>
              </a>
            </Button>
          )}
        </RequireScope>
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

import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useMembers } from "@gram/client/react-query/members.js";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useOrgRoutes } from "@/routes";
import { Users } from "lucide-react";

export function MembersTab() {
  const orgRoutes = useOrgRoutes();
  const { data: membersData } = useMembers();
  const memberCount = membersData?.members?.length ?? 0;

  return (
    <div>
      <div className="mb-4">
        <Heading variant="h4">Team Members</Heading>
        <Type muted small className="mt-1">
          Member management has moved to the Team page.
        </Type>
      </div>

      <div className="border-border bg-muted/20 flex flex-col items-center gap-4 rounded-lg border py-12">
        <Users className="text-muted-foreground h-10 w-10" />
        <div className="text-center">
          <Type variant="body" className="font-medium">
            {memberCount} team member{memberCount === 1 ? "" : "s"}
          </Type>
          <Type muted small className="mt-1">
            Invite, remove, and manage roles for your team in one place.
          </Type>
        </div>
        <Button size="sm" onClick={() => orgRoutes.team.goTo()}>
          <Button.Text>Go to Team</Button.Text>
          <Button.RightIcon>
            <Icon name="arrow-right" className="h-4 w-4" />
          </Button.RightIcon>
        </Button>
      </div>
    </div>
  );
}

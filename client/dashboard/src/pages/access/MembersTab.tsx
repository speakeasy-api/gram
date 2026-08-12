import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { useMembers } from "@gram/client/react-query/members.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useOrgRoutes } from "@/routes";
import { Users } from "lucide-react";

export function MembersTab(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { data: membersData } = useMembers();
  const memberCount = membersData?.members?.length ?? 0;

  return (
    <div>
      <div className="mb-4">
        <Heading variant="h4">Team Members</Heading>
        <Text muted small className="mt-1">
          Member management has moved to the Team page.
        </Text>
      </div>

      <div className="border-border bg-muted/20 flex flex-col items-center gap-4 border py-12">
        <Users className="text-muted-foreground h-10 w-10" />
        <div className="text-center">
          <Text variant="body" className="font-medium">
            {memberCount} team member{memberCount === 1 ? "" : "s"}
          </Text>
          <Text muted small className="mt-1">
            Invite, remove, and manage roles for your team in one place.
          </Text>
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

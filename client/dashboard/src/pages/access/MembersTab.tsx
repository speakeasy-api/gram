import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Type } from "@/components/ui/type";
import { useMembers } from "@gram/client/react-query/members.js";
import { Button } from "@/components/ui/moonshine";
import { useOrgRoutes } from "@/routes";
import { ArrowRight, Users } from "lucide-react";

export function MembersTab(): JSX.Element {
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

      <InlineEmptyState
        icon={<Users />}
        title={`${memberCount} team member${memberCount === 1 ? "" : "s"}`}
        description="Invite, remove, and manage roles for your team in one place."
        action={
          <Button size="sm" onClick={() => orgRoutes.team.goTo()}>
            <Button.Text>Go to Team</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        }
      />
    </div>
  );
}

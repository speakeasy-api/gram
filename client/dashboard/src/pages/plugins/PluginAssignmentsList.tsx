import { MemberFacepile } from "@/components/member-facepile";
import { Text } from "@/components/ui/Text";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAudience } from "@gram/client/models/components/pluginaudience.js";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import type { Role } from "@gram/client/models/components/role.js";
import { Users } from "lucide-react";
import { PluginAssignmentRow } from "./PluginAssignmentRow";
import {
  individualMemberFacepile,
  isIndividualMemberPrincipal,
} from "./principals";

// PluginAssignmentsList renders a plugin's current assignments as a bordered
// list: everyone/role/email principals each get a row, while individually
// assigned members collapse into a single face-stack row so a long roster
// doesn't dominate the section.
export function PluginAssignmentsList({
  assignments,
  roleByUrn,
  memberByUrn,
  audienceByUrn,
}: {
  assignments: PluginAssignment[];
  roleByUrn: Map<string, Role>;
  memberByUrn: Map<string, AccessMember>;
  audienceByUrn: Map<string, PluginAudience>;
}): JSX.Element {
  const rowAssignments = assignments.filter(
    (a) => !isIndividualMemberPrincipal(a.principalUrn),
  );

  const facepileMembers = individualMemberFacepile(assignments, memberByUrn);

  return (
    <div className="border-border divide-border divide-y border px-4">
      {rowAssignments.map((assignment) => (
        <PluginAssignmentRow
          key={assignment.id}
          urn={assignment.principalUrn}
          roleByUrn={roleByUrn}
          memberByUrn={memberByUrn}
          audienceByUrn={audienceByUrn}
        />
      ))}
      {facepileMembers.length > 0 && (
        // Same icon-tile + text structure as the principal rows, so the labels
        // line up; the face-stack sits at the row's trailing edge where its
        // variable width can't shift the text column.
        <div className="flex items-center gap-3 py-3">
          <div className="bg-muted text-muted-foreground flex h-9 w-9 shrink-0 items-center justify-center">
            <Users className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <Text as="div" className="truncate font-medium">
              {facepileMembers.length}{" "}
              {facepileMembers.length === 1 ? "member" : "members"}
            </Text>
            <Text as="div" small muted className="truncate">
              Assigned individually
            </Text>
          </div>
          <MemberFacepile members={facepileMembers} />
        </div>
      )}
    </div>
  );
}

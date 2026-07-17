import {
  type FacepileMember,
  MemberFacepile,
} from "@/components/member-facepile";
import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import type { Role } from "@gram/client/models/components/role.js";
import { PluginAssignmentRow } from "./PluginAssignmentRow";

// An individually-assigned member is a "user:<id>" principal — but not the
// "user:all" subject-set, which describes everyone and renders as its own row.
function isIndividualMember(urn: string): boolean {
  return urn.startsWith("user:") && urn !== "user:all";
}

// PluginAssignmentsList renders a plugin's current assignments as a bordered
// list: everyone/role/email principals each get a row, while individually
// assigned members collapse into a single face-stack row so a long roster
// doesn't dominate the section.
export function PluginAssignmentsList({
  assignments,
  roleByUrn,
  memberByUrn,
}: {
  assignments: PluginAssignment[];
  roleByUrn: Map<string, Role>;
  memberByUrn: Map<string, AccessMember>;
}): JSX.Element {
  const rowAssignments = assignments.filter(
    (a) => !isIndividualMember(a.principalUrn),
  );
  const memberAssignments = assignments.filter((a) =>
    isIndividualMember(a.principalUrn),
  );

  const facepileMembers: FacepileMember[] = memberAssignments.map((a) => {
    const member = memberByUrn.get(a.principalUrn);
    return {
      id: member?.id ?? a.principalUrn,
      name: member?.name || member?.email || "Unknown member",
      email: member?.email ?? "",
      photoUrl: member?.photoUrl,
    };
  });

  return (
    <div className="border-border divide-border divide-y rounded-xl border px-4">
      {rowAssignments.map((assignment) => (
        <PluginAssignmentRow
          key={assignment.id}
          urn={assignment.principalUrn}
          roleByUrn={roleByUrn}
          memberByUrn={memberByUrn}
        />
      ))}
      {facepileMembers.length > 0 && (
        <div className="flex items-center gap-3 py-3">
          <MemberFacepile members={facepileMembers} />
          <div className="min-w-0">
            <Type as="div" className="truncate font-medium">
              {facepileMembers.length}{" "}
              {facepileMembers.length === 1 ? "member" : "members"}
            </Type>
            <Type as="div" small muted className="truncate">
              Assigned individually
            </Type>
          </div>
        </div>
      )}
    </div>
  );
}

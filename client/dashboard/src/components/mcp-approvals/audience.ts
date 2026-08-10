import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";

/**
 * The grouped role/member options an approval audience is picked from. Values
 * are principal URNs — exactly what recordDecision's granted_principal_urns
 * accepts.
 */
type AudienceGroup = {
  heading: string;
  options: { label: string; value: string }[];
};

export function audienceGroups(
  members: AccessMember[],
  roles: Role[],
): AudienceGroup[] {
  return [
    {
      heading: "Roles",
      options: roles.map((role) => ({
        label: role.name,
        value: role.principalUrn,
      })),
    },
    {
      heading: "Members",
      options: members.map((member) => ({
        label:
          member.name && member.name !== member.email
            ? `${member.name} (${member.email})`
            : member.email,
        value: member.principalUrn,
      })),
    },
  ].filter((group) => group.options.length > 0);
}

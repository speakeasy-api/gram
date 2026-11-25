import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";

// Names and sort orders for the members and roles a policy audience or an
// approver list is picked from.

export function memberDisplayName(member: AccessMember): string {
  return member.name || member.email;
}

export function compareMembersByName(a: AccessMember, b: AccessMember): number {
  return memberDisplayName(a).localeCompare(memberDisplayName(b));
}

export function compareRolesByName(a: Role, b: Role): number {
  return a.name.localeCompare(b.name);
}

import type { AccessMember } from "@gram/client/models/components/accessmember.js";

/**
 * Every role a member holds: assigned directly, or granted through a
 * directory role mapping. Read `roleIds` alone only where direct assignments
 * are being edited, since a mapped role follows the member's directory groups
 * and attributes and cannot be removed from the member.
 */
export function allMemberRoleIds(
  member: Pick<AccessMember, "roleIds" | "directoryRoleIds">,
): string[] {
  return [...new Set([...member.roleIds, ...member.directoryRoleIds])];
}

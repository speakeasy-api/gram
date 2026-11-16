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

/**
 * Whether a member belongs to a directory group. Matched by the group's ID,
 * since two directories can each have a group with the same name. Falls back
 * to the name only when the member listing carries no group IDs.
 */
export function memberInDirectoryGroup(
  member: Pick<AccessMember, "groups" | "directoryGroups">,
  group: { id: string; name: string },
): boolean {
  if (member.directoryGroups) {
    return member.directoryGroups.some((g) => g.id === group.id);
  }
  return group.name !== "" && (member.groups ?? []).includes(group.name);
}

/** Where one of a member's roles comes from. A role can have several sources. */
export type MemberRoleSource = {
  /** Assigned to the member directly. */
  direct: boolean;
  /** Granted through a directory role mapping. */
  fromDirectory: boolean;
  /** Names of the directory groups whose mappings grant the role. */
  groupNames: string[];
  /** How many directory attribute values' mappings grant the role. */
  attributeCount: number;
};

export function memberRoleSource(
  member: Pick<
    AccessMember,
    "roleIds" | "directoryRoleIds" | "directoryGroups" | "directoryRoleSources"
  >,
  roleId: string,
): MemberRoleSource {
  const groupNames = new Map(
    (member.directoryGroups ?? []).map((group) => [group.id, group.name]),
  );
  const names = new Set<string>();
  let attributeCount = 0;
  for (const source of member.directoryRoleSources ?? []) {
    if (source.roleId !== roleId) continue;
    if (source.sourceKind === "group") {
      names.add(groupNames.get(source.directoryGroupId ?? "") || "a group");
    } else {
      attributeCount += 1;
    }
  }
  return {
    direct: member.roleIds.includes(roleId),
    fromDirectory: member.directoryRoleIds.includes(roleId),
    groupNames: [...names].sort((a, b) => a.localeCompare(b)),
    attributeCount,
  };
}

function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}

/**
 * The directory part of a role's sources, such as "the Engineering group" or
 * "the Engineering and Platform groups and a directory attribute".
 */
function directorySourceLabel(source: MemberRoleSource): string {
  const parts: string[] = [];
  if (source.groupNames.length > 0) {
    parts.push(
      `the ${joinNames(source.groupNames)} ${source.groupNames.length === 1 ? "group" : "groups"}`,
    );
  }
  if (source.attributeCount > 0) {
    parts.push(
      source.attributeCount === 1
        ? "a directory attribute"
        : `${source.attributeCount} directory attributes`,
    );
  }
  return parts.length > 0 ? joinNames(parts) : "a directory role mapping";
}

/**
 * A sentence naming every source of a role, or undefined when the role is
 * only assigned directly and there is nothing beyond that to explain.
 */
export function describeMemberRoleSource(
  source: MemberRoleSource,
): string | undefined {
  if (!source.fromDirectory) return undefined;
  const directory = directorySourceLabel(source);
  if (source.direct) {
    return `Assigned directly, and also granted by ${directory}. Removing the direct assignment keeps the role while the directory grants it.`;
  }
  return `Granted by ${directory}. It follows the member's directory groups and attributes, so it can't be removed here.`;
}

/** A short form of the sources, for lists such as an overflow tooltip. */
export function shortMemberRoleSource(
  source: MemberRoleSource,
): string | undefined {
  if (!source.fromDirectory) return undefined;
  const groups =
    source.groupNames.length > 0 ? joinNames(source.groupNames) : undefined;
  const directory =
    groups ?? (source.attributeCount > 0 ? "directory attribute" : "directory");
  return source.direct ? `direct and ${directory}` : `via ${directory}`;
}

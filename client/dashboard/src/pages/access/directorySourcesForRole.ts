import type { FacepileMember } from "@/components/member-facepile";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { ListDirectoryRoleMappingsResult } from "@gram/client/models/components/listdirectoryrolemappingsresult.js";

/** One directory group or attribute value that gives out a role. */
export type RoleDirectorySource = {
  key: string;
  kind: "group" | "attribute";
  label: string;
  /** Directory users the source matches, signed in or not. */
  memberCount: number;
  /** Speakeasy members in the group. Attributes are not matched to members. */
  members: FacepileMember[];
};

export type RoleDirectorySources = {
  sources: RoleDirectorySource[];
  /**
   * Members who hold the role directly rather than through a mapping. Under
   * directory sync these come from the identity provider's own role rules,
   * which no mapping here explains.
   */
  unmapped: FacepileMember[];
};

function facepileMember(member: AccessMember): FacepileMember {
  return {
    id: member.id,
    name: member.name,
    email: member.email,
    photoUrl: member.photoUrl,
  };
}

/**
 * The directory groups and attribute values mapped to one role, and the
 * members whose copy of the role no mapping accounts for.
 */
export function roleDirectorySources(
  data: ListDirectoryRoleMappingsResult,
  members: AccessMember[],
  role: { id: string; principalUrn: string },
): RoleDirectorySources {
  const groups = new Map(data.groups.map((group) => [group.id, group]));
  const sources = data.mappings
    .filter((mapping) => mapping.roleUrn === role.principalUrn)
    .map((mapping): RoleDirectorySource => {
      if (mapping.sourceKind === "group") {
        const group = groups.get(mapping.directoryGroupId ?? "");
        const name = group?.name ?? mapping.directoryGroupName ?? "";
        return {
          key: mapping.id,
          kind: "group",
          label: name || "Unnamed group",
          memberCount: group?.memberCount ?? 0,
          // Members name their directory groups, not group ids, so match on
          // the name, as the mappings table does.
          members: name
            ? members
                .filter((member) => member.groups?.includes(name))
                .map(facepileMember)
            : [],
        };
      }
      const key = mapping.attributeKey ?? "";
      const value = mapping.attributeValue ?? "";
      const option = data.attributes.find(
        (a) => a.key === key && a.value === value,
      );
      return {
        key: mapping.id,
        kind: "attribute",
        label: `${key} = ${value}`,
        memberCount: option?.memberCount ?? 0,
        members: [],
      };
    })
    // Groups first, then attributes, each by name.
    .sort(
      (a, b) =>
        Number(a.kind === "attribute") - Number(b.kind === "attribute") ||
        a.label.localeCompare(b.label),
    );

  const unmapped = members
    .filter(
      (member) =>
        member.roleIds.includes(role.id) &&
        !member.directoryRoleIds.includes(role.id),
    )
    .map(facepileMember);

  return { sources, unmapped };
}

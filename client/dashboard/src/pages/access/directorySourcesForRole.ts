import type { FacepileMember } from "@/components/member-facepile";
import { memberInDirectoryGroup } from "@/lib/member-roles";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { ListDirectoryRoleMappingsResult } from "@gram/client/models/components/listdirectoryrolemappingsresult.js";

/** One directory group or attribute value that gives out a role. */
export type RoleDirectorySource = {
  key: string;
  kind: "group" | "attribute";
  label: string;
  /**
   * Directory users the source matches, signed in or not. Undefined when the
   * count is not known (see attributeMemberCount).
   */
  memberCount: number | undefined;
  /** Speakeasy members in the group. Attributes are not matched to members. */
  members: FacepileMember[];
  /**
   * The group was deleted from the directory, or no directory user has the
   * attribute value any more. A missing source grants nothing.
   */
  missing: boolean;
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
 * Directory users holding an attribute value, or undefined when not known. A
 * listed key lists every value its users hold, so a value missing from it
 * matches nobody; a key with too many values is left out whole.
 */
export function attributeMemberCount(
  data: ListDirectoryRoleMappingsResult,
  key: string,
  value: string,
): number | undefined {
  const options = data.attributes.filter((a) => a.key === key);
  if (options.length === 0) return undefined;
  return options.find((a) => a.value === value)?.memberCount ?? 0;
}

/** The directory groups and attribute values mapped to one role. */
export function roleDirectorySources(
  data: ListDirectoryRoleMappingsResult,
  members: AccessMember[],
  role: { principalUrn: string },
): RoleDirectorySource[] {
  const groups = new Map(data.groups.map((group) => [group.id, group]));
  return (
    data.mappings
      .filter((mapping) => mapping.roleUrn === role.principalUrn)
      .map((mapping): RoleDirectorySource => {
        if (mapping.sourceKind === "group") {
          const group = groups.get(mapping.directoryGroupId ?? "");
          if (!group || mapping.sourceStatus === "missing") {
            // The group was deleted from the directory: the mapping matches
            // nobody, and a live group reusing its name is not it.
            const name = mapping.directoryGroupName;
            return {
              key: mapping.id,
              kind: "group",
              label: name ? `${name} (removed)` : "Removed group",
              memberCount: 0,
              members: [],
              missing: true,
            };
          }
          return {
            key: mapping.id,
            kind: "group",
            label: group.name || "Unnamed group",
            memberCount: group.memberCount,
            members: members
              .filter((member) => memberInDirectoryGroup(member, group))
              .map(facepileMember),
            missing: false,
          };
        }
        const key = mapping.attributeKey ?? "";
        const value = mapping.attributeValue ?? "";
        const missing = mapping.sourceStatus === "missing";
        return {
          key: mapping.id,
          kind: "attribute",
          label: missing ? `${key} = ${value} (no users)` : `${key} = ${value}`,
          memberCount: missing ? 0 : attributeMemberCount(data, key, value),
          members: [],
          missing,
        };
      })
      // Groups first, then attributes, each by name.
      .sort(
        (a, b) =>
          Number(a.kind === "attribute") - Number(b.kind === "attribute") ||
          a.label.localeCompare(b.label),
      )
  );
}

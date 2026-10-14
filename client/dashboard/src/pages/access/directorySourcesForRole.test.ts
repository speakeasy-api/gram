import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import type { ListDirectoryRoleMappingsResult } from "@gram/client/models/components/listdirectoryrolemappingsresult.js";
import { describe, expect, it } from "vitest";
import { roleDirectorySources } from "./directorySourcesForRole";

const analyst = { id: "role-analyst", principalUrn: "role:analyst" };

function mapping(
  fields: Partial<DirectoryRoleMapping> &
    Pick<DirectoryRoleMapping, "id" | "sourceKind">,
): DirectoryRoleMapping {
  return {
    roleUrn: analyst.principalUrn,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...fields,
  };
}

function member(
  fields: Partial<AccessMember> & Pick<AccessMember, "id" | "name">,
): AccessMember {
  return {
    email: `${fields.id}@example.com`,
    principalUrn: `user:${fields.id}`,
    joinedAt: new Date(0),
    roleIds: [],
    directoryRoleIds: [],
    ...fields,
  };
}

const data: ListDirectoryRoleMappingsResult = {
  groups: [
    { id: "g-finance", name: "Finance Ops", memberCount: 6 },
    { id: "g-data", name: "Data Analytics", memberCount: 14 },
    { id: "g-eng", name: "Engineering", memberCount: 22 },
  ],
  attributes: [{ key: "department", value: "Research", memberCount: 9 }],
  mappings: [
    mapping({
      id: "m-attr",
      sourceKind: "attribute",
      attributeKey: "department",
      attributeValue: "Research",
    }),
    mapping({
      id: "m-finance",
      sourceKind: "group",
      directoryGroupId: "g-finance",
    }),
    mapping({ id: "m-data", sourceKind: "group", directoryGroupId: "g-data" }),
    mapping({
      id: "m-eng",
      sourceKind: "group",
      directoryGroupId: "g-eng",
      roleUrn: "role:collaborator",
    }),
  ],
};

describe("roleDirectorySources", () => {
  it("lists only the role's mappings, groups before attributes, by name", () => {
    const { sources } = roleDirectorySources(data, [], analyst);
    expect(sources.map((s) => [s.kind, s.label, s.memberCount])).toEqual([
      ["group", "Data Analytics", 14],
      ["group", "Finance Ops", 6],
      ["attribute", "department = Research", 9],
    ]);
  });

  it("puts members on the groups they belong to", () => {
    const members = [
      member({ id: "ava", name: "Ava", groups: ["Data Analytics"] }),
      member({ id: "ben", name: "Ben", groups: ["Engineering"] }),
    ];
    const { sources } = roleDirectorySources(data, members, analyst);
    expect(sources[0]?.members.map((m) => m.id)).toEqual(["ava"]);
    expect(sources[1]?.members).toEqual([]);
  });

  it("falls back to the mapping's group name when the group is gone", () => {
    const { sources } = roleDirectorySources(
      {
        ...data,
        groups: [],
        mappings: [
          mapping({
            id: "m-old",
            sourceKind: "group",
            directoryGroupId: "g-old",
            directoryGroupName: "Old Team",
          }),
        ],
      },
      [],
      analyst,
    );
    expect(sources).toMatchObject([{ label: "Old Team", memberCount: 0 }]);
  });

  it("names members whose role no mapping explains", () => {
    const members = [
      member({ id: "direct", name: "Direct", roleIds: [analyst.id] }),
      member({
        id: "mapped",
        name: "Mapped",
        directoryRoleIds: [analyst.id],
      }),
      member({
        id: "both",
        name: "Both",
        roleIds: [analyst.id],
        directoryRoleIds: [analyst.id],
      }),
      member({ id: "other", name: "Other", roleIds: ["role-other"] }),
    ];
    const { unmapped } = roleDirectorySources(data, members, analyst);
    expect(unmapped.map((m) => m.id)).toEqual(["direct"]);
  });
});

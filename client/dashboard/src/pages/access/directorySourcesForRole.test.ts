import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import type { ListDirectoryRoleMappingsResult } from "@gram/client/models/components/listdirectoryrolemappingsresult.js";
import { describe, expect, it } from "vitest";
import { roleDirectorySources } from "./directorySourcesForRole";

const analyst = { principalUrn: "role:analyst" };

function mapping(
  fields: Partial<DirectoryRoleMapping> &
    Pick<DirectoryRoleMapping, "id" | "sourceKind">,
): DirectoryRoleMapping {
  return {
    roleUrn: analyst.principalUrn,
    sourceStatus: "active",
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
    const sources = roleDirectorySources(data, [], analyst);
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
    const sources = roleDirectorySources(data, members, analyst);
    expect(sources[0]?.members.map((m) => m.id)).toEqual(["ava"]);
    expect(sources[1]?.members).toEqual([]);
  });

  it("marks a deleted group removed, even if a live group reuses its name", () => {
    const sources = roleDirectorySources(
      {
        ...data,
        groups: [{ id: "g-new", name: "Old Team", memberCount: 3 }],
        mappings: [
          mapping({
            id: "m-old",
            sourceKind: "group",
            directoryGroupId: "g-old",
            directoryGroupName: "Old Team",
          }),
        ],
      },
      [member({ id: "ava", name: "Ava", groups: ["Old Team"] })],
      analyst,
    );
    expect(sources).toMatchObject([
      { label: "Old Team (removed)", memberCount: 0, members: [] },
    ]);
  });

  it("counts nobody for a value its listed key no longer holds", () => {
    const sources = roleDirectorySources(
      {
        ...data,
        mappings: [
          mapping({
            id: "m-gone",
            sourceKind: "attribute",
            attributeKey: "department",
            attributeValue: "Legal",
          }),
        ],
      },
      [],
      analyst,
    );
    expect(sources).toEqual([
      expect.objectContaining({
        label: "department = Legal",
        memberCount: 0,
      }),
    ]);
  });

  it("leaves the count unknown when the attribute key is not listed", () => {
    const sources = roleDirectorySources(
      {
        ...data,
        mappings: [
          mapping({
            id: "m-unlisted",
            sourceKind: "attribute",
            attributeKey: "employee_id",
            attributeValue: "1234",
          }),
        ],
      },
      [],
      analyst,
    );
    expect(sources).toEqual([
      expect.objectContaining({
        label: "employee_id = 1234",
        memberCount: undefined,
      }),
    ]);
  });
});

import {
  allMemberRoleIds,
  describeMemberRoleSource,
  memberInDirectoryGroup,
  memberRoleSource,
  shortMemberRoleSource,
} from "./member-roles";
import { describe, expect, it } from "vitest";

describe("allMemberRoleIds", () => {
  it("combines direct and directory-mapped roles once each", () => {
    expect(
      allMemberRoleIds({
        roleIds: ["admin", "builder"],
        directoryRoleIds: ["builder", "viewer"],
      }),
    ).toEqual(["admin", "builder", "viewer"]);
  });
});

describe("memberInDirectoryGroup", () => {
  const engineering = { id: "g-1", name: "Engineering" };

  it("matches by group ID, not name", () => {
    expect(
      memberInDirectoryGroup(
        {
          groups: ["Engineering"],
          directoryGroups: [{ id: "g-2", name: "Engineering" }],
        },
        engineering,
      ),
    ).toBe(false);
    expect(
      memberInDirectoryGroup(
        { directoryGroups: [{ id: "g-1", name: "Engineering" }] },
        engineering,
      ),
    ).toBe(true);
  });

  it("falls back to the name when the listing has no group IDs", () => {
    expect(
      memberInDirectoryGroup({ groups: ["Engineering"] }, engineering),
    ).toBe(true);
  });
});

describe("memberRoleSource", () => {
  const member = {
    roleIds: ["builder"],
    directoryRoleIds: ["builder", "viewer"],
    directoryGroups: [
      { id: "g-eng", name: "Engineering" },
      { id: "g-plat", name: "Platform" },
    ],
    directoryRoleSources: [
      {
        roleId: "builder",
        mappingId: "m-1",
        sourceKind: "group" as const,
        directoryGroupId: "g-eng",
      },
      {
        roleId: "viewer",
        mappingId: "m-2",
        sourceKind: "group" as const,
        directoryGroupId: "g-plat",
      },
      {
        roleId: "viewer",
        mappingId: "m-3",
        sourceKind: "group" as const,
        directoryGroupId: "g-eng",
      },
      { roleId: "viewer", mappingId: "m-4", sourceKind: "attribute" as const },
    ],
  };

  it("reports a role held both directly and through a group", () => {
    const source = memberRoleSource(member, "builder");
    expect(source).toEqual({
      direct: true,
      fromDirectory: true,
      groupNames: ["Engineering"],
      attributeCount: 0,
    });
    expect(describeMemberRoleSource(source)).toMatch(
      /^Assigned directly, and also granted by the Engineering group\./,
    );
    expect(shortMemberRoleSource(source)).toBe("direct and Engineering");
  });

  it("names every source of a mapped role", () => {
    const source = memberRoleSource(member, "viewer");
    expect(source.groupNames).toEqual(["Engineering", "Platform"]);
    expect(source.attributeCount).toBe(1);
    expect(describeMemberRoleSource(source)).toMatch(
      /^Granted by the Engineering and Platform groups and a directory attribute\./,
    );
    expect(shortMemberRoleSource(source)).toBe("via Engineering and Platform");
  });

  it("explains nothing for a role that is only assigned directly", () => {
    const source = memberRoleSource(
      { ...member, roleIds: ["admin"], directoryRoleIds: [] },
      "admin",
    );
    expect(describeMemberRoleSource(source)).toBeUndefined();
    expect(shortMemberRoleSource(source)).toBeUndefined();
  });
});

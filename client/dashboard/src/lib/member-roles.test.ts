import { describe, expect, it } from "vitest";
import { allMemberRoleIds } from "./member-roles";

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

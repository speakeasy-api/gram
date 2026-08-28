import { describe, expect, it } from "vitest";

import { getMemberKillswitchMenuModel } from "./team-member-killswitch-menu";

describe("Team member Killswitch menu model", () => {
  it("uses the exact member ID for both view and create URLs", () => {
    expect(
      getMemberKillswitchMenuModel("/example/killswitch", "member-42"),
    ).toEqual({
      viewHref: "/example/killswitch?user=member-42",
      newHref: "/example/killswitch?create=1&createUser=member-42",
    });
  });
});

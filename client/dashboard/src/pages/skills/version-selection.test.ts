import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { describe, expect, it } from "vitest";
import { selectDiffVersions } from "./version-selection";

function version(id: string): SkillVersion {
  return {
    id,
    skillId: "skill_a",
    content: id,
    canonicalSha256: id.padEnd(64, "0"),
    rawSha256: id.padEnd(64, "1"),
    createdAt: new Date("2026-07-16T00:00:00.000Z"),
    createdByUserId: "user_a",
    metadata: {},
    specValid: true,
    validationErrors: [],
  };
}

describe("selectDiffVersions", () => {
  const newest = version("newest");
  const middle = version("middle");
  const oldest = version("oldest");
  const newestFirst = [newest, middle, oldest];

  it("uses API newest-first order when timestamps tie", () => {
    expect(
      selectDiffVersions(newestFirst, new Set(["newest", "oldest"]), "newest"),
    ).toEqual([oldest, newest]);
    expect(
      selectDiffVersions(newestFirst, new Set(["middle", "oldest"]), "newest"),
    ).toEqual([oldest, middle]);
  });

  it("compares one selected older version with latest", () => {
    expect(
      selectDiffVersions(newestFirst, new Set(["middle"]), "newest"),
    ).toEqual([middle, newest]);
  });
});

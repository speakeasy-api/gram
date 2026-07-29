import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { describe, expect, it } from "vitest";
import {
  selectDiffVersions,
  versionChangeDirection,
} from "./version-selection";

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
    frontmatter: {},
    specValid: true,
    validationErrors: [],
    seenCount: 0,
  };
}

describe("versionChangeDirection", () => {
  const current = version("middle");

  it("uses creation order to distinguish rollbacks from roll-forwards", () => {
    expect(
      versionChangeDirection(
        { ...version("older"), createdAt: new Date("2026-07-15T00:00:00Z") },
        current,
      ),
    ).toBe("backward");
    expect(
      versionChangeDirection(
        { ...version("newer"), createdAt: new Date("2026-07-17T00:00:00Z") },
        current,
      ),
    ).toBe("forward");
  });

  it("matches the API id ordering when creation times tie", () => {
    expect(versionChangeDirection(version("lower"), current)).toBe("backward");
    expect(versionChangeDirection(version("newer"), current)).toBe("forward");
    expect(versionChangeDirection(current, current)).toBeNull();
  });
});

describe("selectDiffVersions", () => {
  const newest = version("newest");
  const middle = version("middle");
  const oldest = version("oldest");
  const newestFirst = [newest, middle, oldest];

  it("uses API newest-first order when timestamps tie", () => {
    expect(
      selectDiffVersions(newestFirst, new Set(["newest", "oldest"]), newest),
    ).toEqual([oldest, newest]);
    expect(
      selectDiffVersions(newestFirst, new Set(["middle", "oldest"]), newest),
    ).toEqual([oldest, middle]);
  });

  it("compares one selected older version with current", () => {
    expect(
      selectDiffVersions(newestFirst, new Set(["middle"]), newest),
    ).toEqual([middle, newest]);
  });

  it("compares one selected newer version with current", () => {
    expect(
      selectDiffVersions(newestFirst, new Set(["newest"]), middle),
    ).toEqual([middle, newest]);
  });

  it("compares with a current version outside the loaded page", () => {
    expect(selectDiffVersions([newest], new Set(["newest"]), middle)).toEqual([
      middle,
      newest,
    ]);
  });
});

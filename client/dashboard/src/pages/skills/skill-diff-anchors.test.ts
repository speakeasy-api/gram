import { describe, expect, it } from "vitest";
import { groupHunksByAnchor, parseSkillDiffHunks } from "./skill-diff-anchors";

const replacement = `--- a/SKILL.md
+++ b/SKILL.md
@@ -12,4 +12,4 @@
 Line seven.
 Line eight.
 Line nine.
-Line ten.
+Line ten, with detail.
`;

const insertion = `--- a/SKILL.md
+++ b/SKILL.md
@@ -3,3 +3,4 @@
 Write a blameless narrative.
+Quantify impact.
 Produce a five-whys section.
 List action items.
`;

describe("parseSkillDiffHunks", () => {
  it("anchors a replacement to the line it replaces", () => {
    expect(parseSkillDiffHunks(replacement)).toEqual([
      {
        anchorLine: 15,
        removed: ["Line ten."],
        added: ["Line ten, with detail."],
      },
    ]);
  });

  it("anchors an insertion to the line it follows", () => {
    expect(parseSkillDiffHunks(insertion)).toEqual([
      { anchorLine: 3, removed: [], added: ["Quantify impact."] },
    ]);
  });

  it("ignores file headers and empty diffs", () => {
    expect(parseSkillDiffHunks("")).toEqual([]);
    expect(parseSkillDiffHunks("--- a/SKILL.md\n+++ b/SKILL.md\n")).toEqual([]);
  });
});

describe("groupHunksByAnchor", () => {
  it("collapses hunks sharing a line into one marker", () => {
    const anchors = groupHunksByAnchor([
      { anchorLine: 15, removed: ["a"], added: ["b"] },
      { anchorLine: 3, removed: [], added: ["c"] },
      { anchorLine: 15, removed: [], added: ["d"] },
    ]);

    expect(anchors.map((anchor) => [anchor.line, anchor.hunks.length])).toEqual(
      [
        [3, 1],
        [15, 2],
      ],
    );
  });
});

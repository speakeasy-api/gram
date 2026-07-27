import { describe, expect, it } from "vitest";
import { parseSkillDiffHunks } from "./skill-diff-anchors";

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

const deletion = `--- a/SKILL.md
+++ b/SKILL.md
@@ -3,4 +3,3 @@
 Write a blameless narrative.
-Attach the raw timeline.
 Produce a five-whys section.
 List action items.
`;

describe("parseSkillDiffHunks", () => {
  it("anchors a replacement to the added line", () => {
    expect(parseSkillDiffHunks(replacement)).toEqual([
      {
        side: "additions",
        line: 15,
        removed: ["Line ten."],
        added: ["Line ten, with detail."],
      },
    ]);
  });

  it("anchors an insertion to the line it introduces", () => {
    expect(parseSkillDiffHunks(insertion)).toEqual([
      { side: "additions", line: 4, removed: [], added: ["Quantify impact."] },
    ]);
  });

  it("anchors a pure deletion to the removed line", () => {
    expect(parseSkillDiffHunks(deletion)).toEqual([
      {
        side: "deletions",
        line: 4,
        removed: ["Attach the raw timeline."],
        added: [],
      },
    ]);
  });

  it("ignores file headers and empty diffs", () => {
    expect(parseSkillDiffHunks("")).toEqual([]);
    expect(parseSkillDiffHunks("--- a/SKILL.md\n+++ b/SKILL.md\n")).toEqual([]);
  });
});

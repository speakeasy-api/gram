const HUNK_HEADER = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/** Which side of a rendered diff an anchor belongs to. */
export type SkillDiffSide = "additions" | "deletions";

export type SkillDiffHunk = {
  side: SkillDiffSide;
  /** 1-based line number on that side of the diff. */
  line: number;
  removed: string[];
  added: string[];
};

/**
 * Splits a unified diff into hunks addressed by the line numbers a rendered
 * diff uses, so a review comment can hang off the line it talks about. A hunk
 * anchors to its first added line, falling back to its first removed line when
 * the hunk only deletes.
 */
export function parseSkillDiffHunks(diff: string): SkillDiffHunk[] {
  const hunks: SkillDiffHunk[] = [];
  let current: SkillDiffHunk | null = null;
  let oldLine = 0;
  let newLine = 0;
  let anchored = false;

  for (const line of diff.split("\n")) {
    const header = HUNK_HEADER.exec(line);
    if (header) {
      oldLine = Number(header[1]);
      newLine = Number(header[2]);
      anchored = false;
      current = { side: "additions", line: newLine, removed: [], added: [] };
      hunks.push(current);
      continue;
    }
    if (!current) continue;

    if (line.startsWith("+")) {
      current.added.push(line.slice(1));
      if (!anchored || current.side === "deletions") {
        current.side = "additions";
        current.line = newLine;
        anchored = true;
      }
      newLine += 1;
    } else if (line.startsWith("-")) {
      current.removed.push(line.slice(1));
      if (!anchored) {
        current.side = "deletions";
        current.line = oldLine;
        anchored = true;
      }
      oldLine += 1;
    } else if (line.startsWith(" ")) {
      oldLine += 1;
      newLine += 1;
    }
  }

  return hunks.filter(
    (hunk) => hunk.removed.length > 0 || hunk.added.length > 0,
  );
}

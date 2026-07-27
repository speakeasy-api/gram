const HUNK_HEADER = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

export type SkillDiffHunk = {
  /** 1-based line in the current manifest the hunk is anchored to. */
  anchorLine: number;
  removed: string[];
  added: string[];
};

/**
 * Splits a unified diff into hunks anchored to lines of the manifest it was
 * generated against, which is what the review gutter hangs its markers on.
 * Lines before the first hunk header are the file header and are ignored.
 */
export function parseSkillDiffHunks(diff: string): SkillDiffHunk[] {
  const hunks: SkillDiffHunk[] = [];
  let current: SkillDiffHunk | null = null;
  let currentLine = 0;
  let anchored = false;

  for (const line of diff.split("\n")) {
    const header = HUNK_HEADER.exec(line);
    if (header) {
      currentLine = Number(header[1]);
      anchored = false;
      current = { anchorLine: currentLine, removed: [], added: [] };
      hunks.push(current);
      continue;
    }
    if (!current) continue;

    if (line.startsWith("-")) {
      current.removed.push(line.slice(1));
      if (!anchored) {
        current.anchorLine = currentLine;
        anchored = true;
      }
      currentLine += 1;
    } else if (line.startsWith("+")) {
      current.added.push(line.slice(1));
      if (!anchored) {
        // A pure insertion hangs off the line it follows.
        current.anchorLine = Math.max(1, currentLine - 1);
        anchored = true;
      }
    } else if (line.startsWith(" ")) {
      currentLine += 1;
    }
  }

  return hunks.filter(
    (hunk) => hunk.removed.length > 0 || hunk.added.length > 0,
  );
}

export type SkillDiffAnchor = {
  line: number;
  hunks: SkillDiffHunk[];
};

/** Groups hunks that resolve to the same manifest line into one marker. */
export function groupHunksByAnchor(hunks: SkillDiffHunk[]): SkillDiffAnchor[] {
  const byLine = new Map<number, SkillDiffHunk[]>();
  for (const hunk of hunks) {
    const existing = byLine.get(hunk.anchorLine);
    if (existing) {
      existing.push(hunk);
    } else {
      byLine.set(hunk.anchorLine, [hunk]);
    }
  }

  return [...byLine.entries()]
    .map(([line, grouped]) => ({ line, hunks: grouped }))
    .sort((a, b) => a.line - b.line);
}

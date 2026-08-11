import type { SkillEditSuggestionChange } from "@gram/client/models/components/skilleditsuggestionchange.js";

/** Which side of a rendered diff an anchor belongs to. */
type SkillDiffSide = "additions" | "deletions";

export type SkillDiffAnchor = {
  side: SkillDiffSide;
  /** 1-based line number on that side of the rendered diff. */
  line: number;
  change: SkillEditSuggestionChange;
};

/**
 * Finds the first line a change adds, falling back to the first line it removes
 * when the change only deletes.
 */
function firstEditedLine(
  diff: string,
): { side: SkillDiffSide; text: string } | null {
  let removed: string | null = null;

  for (const line of diff.split("\n")) {
    if (line.startsWith("+++") || line.startsWith("---")) continue;
    if (line.startsWith("+")) {
      return { side: "additions", text: line.slice(1) };
    }
    if (line.startsWith("-") && removed === null) {
      removed = line.slice(1);
    }
  }

  return removed === null ? null : { side: "deletions", text: removed };
}

/**
 * Locates a change in the diff the reviewer is looking at. A change is stored
 * as a diff against whatever the changes before it produce, so its own line
 * numbers do not line up with the rendered current-to-proposed diff. The edited
 * line is found by its text in the content that side of the diff renders, which
 * holds however the changes are ordered or spaced.
 */
export function changeAnchor(
  change: SkillEditSuggestionChange,
  currentContent: string,
  proposedContent: string,
): SkillDiffAnchor | null {
  const edited = firstEditedLine(change.proposedDiff);
  if (!edited) return null;

  const haystack =
    edited.side === "additions" ? proposedContent : currentContent;
  const line = haystack.split("\n").indexOf(edited.text);
  if (line === -1) return null;

  return { side: edited.side, line: line + 1, change };
}

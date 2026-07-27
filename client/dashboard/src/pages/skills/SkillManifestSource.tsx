import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import type { SkillDiffAnchor } from "./skill-diff-anchors";

/**
 * Renders SKILL.md exactly as agents load it, one line per row, with a review
 * gutter beside the lines a suggestion changes. Line numbers address the stored
 * manifest, so this renders the raw source including frontmatter.
 */
export default function SkillManifestSource({
  content,
  anchors,
  renderGutter,
  renderAnchor,
}: {
  content: string;
  anchors: SkillDiffAnchor[];
  renderGutter: (anchor: SkillDiffAnchor) => ReactNode;
  renderAnchor: (anchor: SkillDiffAnchor) => ReactNode;
}): JSX.Element {
  const lines = content.replace(/\n$/, "").split("\n");
  const anchorsByLine = new Map(anchors.map((anchor) => [anchor.line, anchor]));

  return (
    <ol className="font-mono text-xs">
      {lines.map((line, index) => {
        const lineNumber = index + 1;
        const anchor = anchorsByLine.get(lineNumber);

        return (
          <li key={lineNumber}>
            <div
              className={cn(
                "grid grid-cols-[2.25rem_2.5rem_1fr] items-start",
                anchor && "bg-muted/40",
              )}
            >
              <span className="flex justify-center py-0.5">
                {anchor ? renderGutter(anchor) : null}
              </span>
              <span className="text-muted-foreground/60 py-0.5 pr-3 text-right select-none">
                {lineNumber}
              </span>
              <code className="py-0.5 pr-4 break-words whitespace-pre-wrap">
                {line || " "}
              </code>
            </div>
            {anchor ? renderAnchor(anchor) : null}
          </li>
        );
      })}
    </ol>
  );
}

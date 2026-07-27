import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import type { SkillDiffAnchor } from "./skill-diff-anchors";

type ManifestPlan = {
  /** Manifest lines the suggestion deletes, rendered as removals in place. */
  removed: Set<number>;
  /** Lines the suggestion adds, rendered after the last line each hunk spans. */
  added: Map<number, { lines: string[]; anchor: SkillDiffAnchor }>;
};

function buildPlan(anchors: SkillDiffAnchor[]): ManifestPlan {
  const removed = new Set<number>();
  const added = new Map<number, { lines: string[]; anchor: SkillDiffAnchor }>();

  for (const anchor of anchors) {
    const lines: string[] = [];
    let last = anchor.line;
    for (const hunk of anchor.hunks) {
      for (let offset = 0; offset < hunk.removed.length; offset += 1) {
        removed.add(anchor.line + offset);
        last = Math.max(last, anchor.line + offset);
      }
      lines.push(...hunk.added);
    }
    added.set(last, { lines, anchor });
  }

  return { removed, added };
}

/**
 * Renders SKILL.md exactly as agents load it, one line per row. A suggestion is
 * shown in place: the lines it removes are struck through the manifest, the
 * lines it adds follow them, and the review comment hangs off the same anchor.
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
  const plan = buildPlan(anchors);
  // The marker column only earns its width when something is anchored.
  const columns = anchors.length > 0 ? "1.75rem auto 1fr" : "auto 1fr";
  const numberWidth = `${String(lines.length).length + 1}ch`;

  return (
    <ol className="font-mono text-xs leading-5">
      {lines.map((line, index) => {
        const lineNumber = index + 1;
        const anchor = anchorsByLine.get(lineNumber);
        const addition = plan.added.get(lineNumber);

        return (
          <li key={lineNumber}>
            <div
              className={cn(
                "grid items-start",
                plan.removed.has(lineNumber) &&
                  "bg-destructive/10 text-destructive",
              )}
              style={{ gridTemplateColumns: columns }}
            >
              {anchors.length > 0 && (
                <span className="flex justify-center">
                  {anchor ? renderGutter(anchor) : null}
                </span>
              )}
              <span
                className="text-muted-foreground/60 pr-3 text-right select-none"
                style={{ width: numberWidth }}
              >
                {lineNumber}
              </span>
              <code className="break-words whitespace-pre-wrap">
                {line || " "}
              </code>
            </div>

            {addition?.lines.map((added, addedIndex) => (
              <div
                key={`added-${addedIndex}`}
                className="bg-success-softest text-success-default grid items-start"
                style={{ gridTemplateColumns: columns }}
              >
                {anchors.length > 0 && <span />}
                <span
                  className="pr-3 text-right select-none"
                  style={{ width: numberWidth }}
                >
                  +
                </span>
                <code className="break-words whitespace-pre-wrap">
                  {added || " "}
                </code>
              </div>
            ))}

            {addition ? renderAnchor(addition.anchor) : null}
          </li>
        );
      })}
    </ol>
  );
}
